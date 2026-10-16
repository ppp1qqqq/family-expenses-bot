// Package agent превращает сообщение пользователя в действия над его тратами через LLM с инструментами.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ivanmorozov/family-expenses/internal/llm"
	"github.com/ivanmorozov/family-expenses/internal/service"
)

type ChatModel interface {
	Chat(ctx context.Context, messages []llm.Message, tools []llm.Tool) (llm.Message, error)
}

// DefaultMaxSteps ограничивает число обращений к модели на одно сообщение пользователя.
const DefaultMaxSteps = 6

const FallbackReply = "Не получилось разобрать запрос. Попробуйте сформулировать иначе, например «кофе 350» или «сколько потратил за неделю»."

type Agent struct {
	model    ChatModel
	svc      *service.Service
	log      *slog.Logger
	maxSteps int
}

func New(model ChatModel, svc *service.Service, log *slog.Logger) *Agent {
	return &Agent{model: model, svc: svc, log: log, maxSteps: DefaultMaxSteps}
}

// Reply обрабатывает одно сообщение. userID берётся из Telegram-апдейта, модель его не видит и не может подменить.
// Возвращает текст ответа и все сообщения хода, чтобы бот добавил их в историю диалога.
func (a *Agent) Reply(ctx context.Context, userID int64, history []llm.Message, text string) (string, []llm.Message, error) {
	exp := a.svc.ForUser(userID)
	turn := []llm.Message{{Role: "user", Content: text}}

	for step := 0; step < a.maxSteps; step++ {
		msgs := make([]llm.Message, 0, len(history)+len(turn)+1)
		msgs = append(msgs, llm.Message{Role: "system", Content: a.systemPrompt()})
		msgs = append(msgs, history...)
		msgs = append(msgs, turn...)

		resp, err := a.model.Chat(ctx, msgs, tools)
		if err != nil {
			return "", nil, err
		}
		turn = append(turn, resp)

		if len(resp.ToolCalls) == 0 {
			reply := strings.TrimSpace(resp.Content)
			if reply == "" {
				reply = FallbackReply
			}
			return reply, turn, nil
		}

		for _, call := range resp.ToolCalls {
			result, err := execute(ctx, exp, call.Function)
			if err != nil {
				return "", nil, fmt.Errorf("tool %s: %w", call.Function.Name, err)
			}
			a.log.Debug("tool call", "user_id", userID, "call", describeCall(call.Function), "result", result)
			turn = append(turn, llm.Message{Role: "tool", ToolCallID: call.ID, Content: result})
		}
	}

	a.log.Warn("agent step limit reached", "user_id", userID, "text", text)
	// Ход обрывается на результатах инструментов; закрываем его ответом, чтобы история осталась корректной.
	turn = append(turn, llm.Message{Role: "assistant", Content: FallbackReply})
	return FallbackReply, turn, nil
}

var weekdays = [...]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}

func (a *Agent) systemPrompt() string {
	today := a.svc.Today()
	return fmt.Sprintf(systemPrompt,
		today.Format("2006-01-02"),
		weekdays[today.Weekday()],
		a.svc.Location().String(),
	)
}

const systemPrompt = `Ты помощник для учёта семейных расходов в Telegram. Сегодня %s, %s, часовой пояс %s.

Пользователь пишет обычным текстом. Через инструменты ты добавляешь, ищешь, считаешь, изменяешь и удаляешь только его траты. Чужих трат для тебя не существует, и других пользователей тоже.

Добавление. Сообщение вида «кофе 350» или «такси 1200 вчера» означает новую трату: вызови add_expense. Категорию выбери по смыслу:
food: продукты, кафе, рестораны, кофе, доставка еды;
transport: такси, метро, автобус, бензин, парковка, каршеринг;
housing: аренда, ипотека, коммунальные платежи, интернет, ремонт;
entertainment: кино, концерты, игры, подписки, хобби, путешествия;
health: аптека, врачи, анализы, спорт;
shopping: одежда, техника, товары для дома, подарки;
other: всё остальное.
В comment коротко запиши, на что потрачено, с маленькой буквы: «кофе», «такси». Если дата не названа, не передавай spent_at.

Даты. Относительные даты («вчера», «в пятницу», «на прошлой неделе») переводи в YYYY-MM-DD от сегодняшней даты. «За неделю» без уточнений означает последние 7 дней включая сегодня. «В этом месяце» означает с 1-го числа по сегодня. Будущие даты не используй.

Подсчёт. Никогда не складывай суммы сам. На вопросы «сколько потратил» вызывай sum_expenses и называй сумму из результата.

Изменение и удаление. Сначала найди трату через find_expenses, даже если она уже видна в истории диалога (данные могли измениться в веб-панели), потом вызывай update_expense или delete_expense с id из результата. «Последняя» или «последнее» означает find_expenses с sort=created_at и limit=1. Если под описание подходит больше одной траты и из сообщения нельзя понять какая, ничего не меняй и не удаляй: покажи варианты с датами и суммами и спроси. Никогда не придумывай id и не показывай id пользователю, он ему ничего не говорит.

Если инструмент вернул error, объясни пользователю проблему простыми словами.
Если сообщение не про расходы, коротко скажи, что ты умеешь: добавлять, показывать, считать, изменять и удалять траты.

Отвечай коротко, по-русски, обычным текстом без Markdown. Суммы пиши как «350 ₽», категории по-русски (еда, транспорт, жильё, развлечения, здоровье, покупки, другое), даты как «16 октября». После добавления подтверди, что записал: на что, сколько, категория, дата.`
