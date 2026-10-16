// Package bot получает сообщения из Telegram и передаёт их ИИ-агенту.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/llm"
	"github.com/ivanmorozov/family-expenses/internal/service"
	"github.com/ivanmorozov/family-expenses/internal/telegram"
)

type Telegram interface {
	GetUpdates(ctx context.Context, offset int64) ([]telegram.Update, error)
	SendMessage(ctx context.Context, chatID int64, text string) error
	SendTyping(ctx context.Context, chatID int64) error
}

type Agent interface {
	Reply(ctx context.Context, userID int64, history []llm.Message, text string) (string, []llm.Message, error)
}

const (
	replyTimeout   = 90 * time.Second
	maxInputRunes  = 1000
	maxOutputRunes = 4000 // у Telegram лимит 4096 символов
	historyTurns   = 6
	historyTTL     = 30 * time.Minute
)

const helpText = `Привет! Я записываю траты. Пишите обычным текстом:

кофе 350
такси 1200 вчера
сколько потратил на еду в этом месяце
покажи траты за неделю
последнее было не 350, а 450
удали такси

Ваши траты видите только вы и глава семьи в веб-панели.`

type Bot struct {
	tg      Telegram
	agent   Agent
	svc     *service.Service
	allowed map[int64]bool // пустая карта пускает всех
	log     *slog.Logger
	hist    *history

	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

func New(tg Telegram, ag Agent, svc *service.Service, allowed []int64, log *slog.Logger) *Bot {
	b := &Bot{
		tg:      tg,
		agent:   ag,
		svc:     svc,
		allowed: map[int64]bool{},
		log:     log,
		hist:    newHistory(historyTurns, historyTTL),
		locks:   map[int64]*sync.Mutex{},
	}
	for _, id := range allowed {
		b.allowed[id] = true
	}
	return b
}

// Run читает апдейты через long polling, пока не отменён ctx.
// Сообщения разных пользователей обрабатываются параллельно, одного пользователя строго по очереди.
func (b *Bot) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()

	var offset int64
	for ctx.Err() == nil {
		updates, err := b.tg.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.log.Error("telegram getUpdates", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message == nil || u.Message.From == nil || u.Message.From.IsBot {
				continue
			}
			msg := *u.Message
			wg.Add(1)
			go func() {
				defer wg.Done()
				b.handle(ctx, msg)
			}()
		}
	}
}

func (b *Bot) userLock(userID int64) *sync.Mutex {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.locks[userID]
	if !ok {
		l = &sync.Mutex{}
		b.locks[userID] = l
	}
	return l
}

func (b *Bot) handle(ctx context.Context, msg telegram.Message) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("panic while handling message", "panic", r)
		}
	}()

	from := msg.From
	lock := b.userLock(from.ID)
	lock.Lock()
	defer lock.Unlock()

	if reply := b.precheck(ctx, msg); reply != "" {
		b.send(ctx, msg.Chat.ID, reply)
		return
	}

	stopTyping := b.keepTyping(ctx, msg.Chat.ID)
	replyCtx, cancel := context.WithTimeout(ctx, replyTimeout)
	reply, turn, err := b.agent.Reply(replyCtx, from.ID, b.hist.get(from.ID), msg.Text)
	cancel()
	stopTyping()

	if err != nil {
		b.log.Error("agent reply", "user_id", from.ID, "err", err)
		reply = "Не получилось обработать сообщение. Попробуйте ещё раз чуть позже."
	} else {
		b.hist.add(from.ID, turn)
	}
	b.send(ctx, msg.Chat.ID, reply)
}

// precheck отвечает на сообщения, которые не нужно отдавать модели. Пустая строка означает «передать агенту».
func (b *Bot) precheck(ctx context.Context, msg telegram.Message) string {
	from := msg.From
	if msg.Chat.Type != "private" {
		return "Я работаю только в личных сообщениях, чтобы траты не были видны другим."
	}
	if len(b.allowed) > 0 && !b.allowed[from.ID] {
		b.log.Info("rejected user", "user_id", from.ID, "username", from.Username)
		return fmt.Sprintf("Этот бот только для членов семьи. Чтобы получить доступ, передайте главе семьи ваш Telegram ID: %d", from.ID)
	}
	if err := b.svc.RegisterUser(ctx, domain.User{
		TelegramID: from.ID,
		FirstName:  from.FirstName,
		Username:   from.Username,
	}); err != nil {
		b.log.Error("register user", "user_id", from.ID, "err", err)
		return "Не получилось обработать сообщение. Попробуйте ещё раз чуть позже."
	}

	text := strings.TrimSpace(msg.Text)
	switch {
	case text == "":
		return "Я понимаю только текст. Напишите, например: кофе 350"
	case text == "/start" || text == "/help":
		b.hist.reset(from.ID)
		return helpText
	case utf8.RuneCountInString(text) > maxInputRunes:
		return "Сообщение слишком длинное. Напишите покороче."
	}
	return ""
}

// keepTyping показывает «печатает…», пока агент думает. Статус в Telegram гаснет через 5 секунд, поэтому обновляем его.
func (b *Bot) keepTyping(ctx context.Context, chatID int64) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		t := time.NewTicker(4 * time.Second)
		defer t.Stop()
		for {
			_ = b.tg.SendTyping(ctx, chatID)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return cancel
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	if utf8.RuneCountInString(text) > maxOutputRunes {
		text = string([]rune(text)[:maxOutputRunes]) + "…"
	}
	if err := b.tg.SendMessage(ctx, chatID, text); err != nil {
		b.log.Error("telegram sendMessage", "chat_id", chatID, "err", err)
	}
}
