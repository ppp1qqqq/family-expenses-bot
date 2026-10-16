package bot

import (
	"sync"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/llm"
)

// history хранит последние ходы диалога каждого пользователя в памяти процесса.
// Ход целиком (сообщение, вызовы инструментов, ответ) хранится вместе: если обрезать его
// посередине, провайдер отклонит запрос из-за результата инструмента без вызова.
type history struct {
	mu       sync.Mutex
	maxTurns int
	ttl      time.Duration
	byUser   map[int64]*dialog
}

type dialog struct {
	turns    [][]llm.Message
	lastSeen time.Time
}

func newHistory(maxTurns int, ttl time.Duration) *history {
	return &history{maxTurns: maxTurns, ttl: ttl, byUser: map[int64]*dialog{}}
}

func (h *history) get(userID int64) []llm.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.byUser[userID]
	if !ok {
		return nil
	}
	if time.Since(d.lastSeen) > h.ttl {
		delete(h.byUser, userID)
		return nil
	}
	var out []llm.Message
	for _, t := range d.turns {
		out = append(out, t...)
	}
	return out
}

func (h *history) add(userID int64, turn []llm.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.byUser[userID]
	if !ok {
		d = &dialog{}
		h.byUser[userID] = d
	}
	d.turns = append(d.turns, turn)
	if len(d.turns) > h.maxTurns {
		d.turns = d.turns[len(d.turns)-h.maxTurns:]
	}
	d.lastSeen = time.Now()
}

func (h *history) reset(userID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.byUser, userID)
}
