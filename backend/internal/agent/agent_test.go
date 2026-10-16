package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/llm"
	"github.com/ivanmorozov/family-expenses/internal/service"
	"github.com/ivanmorozov/family-expenses/internal/storage/memstore"
)

// scriptedModel отдаёт заранее заданные ответы и запоминает, что ей прислали.
type scriptedModel struct {
	replies []llm.Message
	seen    [][]llm.Message
}

func (m *scriptedModel) Chat(_ context.Context, msgs []llm.Message, _ []llm.Tool) (llm.Message, error) {
	m.seen = append(m.seen, msgs)
	if len(m.replies) == 0 {
		return llm.Message{}, fmt.Errorf("script is over")
	}
	r := m.replies[0]
	m.replies = m.replies[1:]
	return r, nil
}

func call(id, name, args string) llm.Message {
	return llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
		ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args},
	}}}
}

func text(s string) llm.Message {
	return llm.Message{Role: "assistant", Content: s}
}

const (
	alice int64 = 1
	bob   int64 = 2
)

func setup(t *testing.T) (*service.Service, *memstore.Store) {
	t.Helper()
	store := memstore.New()
	for _, id := range []int64{alice, bob} {
		if err := store.UpsertUser(context.Background(), domain.User{TelegramID: id, FirstName: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	return service.New(store, time.UTC).WithClock(func() time.Time { return now }), store
}

func newAgent(m ChatModel, svc *service.Service) *Agent {
	return New(m, svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAddExpenseUsesOwnerFromServer(t *testing.T) {
	svc, _ := setup(t)
	model := &scriptedModel{replies: []llm.Message{
		call("c1", "add_expense", `{"amount": 350, "category": "food", "comment": "кофе"}`),
		text("Записал: кофе, 350 ₽, еда, 16 октября."),
	}}

	reply, turn, err := newAgent(model, svc).Reply(context.Background(), alice, nil, "кофе 350")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "350") {
		t.Fatalf("reply = %q", reply)
	}
	// user, assistant(tool call), tool result, assistant(text)
	if len(turn) != 4 || turn[2].Role != "tool" || turn[2].ToolCallID != "c1" {
		t.Fatalf("unexpected turn: %+v", turn)
	}

	res, err := svc.List(context.Background(), domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Items[0].UserID != alice || res.Items[0].Amount != 35000 {
		t.Fatalf("stored expenses: %+v", res.Items)
	}
	if !strings.Contains(model.seen[0][0].Content, "2026-10-16") {
		t.Fatal("system prompt has no today's date")
	}
}

func TestModelCannotDeleteForeignExpense(t *testing.T) {
	svc, _ := setup(t)
	bobs, err := svc.ForUser(bob).Add(context.Background(), service.CreateInput{Amount: 120000, Category: domain.CategoryTransport, Comment: "такси"})
	if err != nil {
		t.Fatal(err)
	}

	// Модель «уговорили» удалить трату по чужому id.
	model := &scriptedModel{replies: []llm.Message{
		call("c1", "delete_expense", fmt.Sprintf(`{"id": %d}`, bobs.ID)),
		text("Такой траты нет."),
	}}
	if _, _, err := newAgent(model, svc).Reply(context.Background(), alice, nil, "удали трату с id 1"); err != nil {
		t.Fatal(err)
	}

	toolResult := model.seen[1][len(model.seen[1])-1]
	if toolResult.Role != "tool" || !strings.Contains(toolResult.Content, "не найдена") {
		t.Fatalf("tool result = %+v", toolResult)
	}
	if _, err := svc.Get(context.Background(), bobs.ID, nil); err != nil {
		t.Fatalf("bob's expense was deleted: %v", err)
	}
}

func TestUpdateLastExpense(t *testing.T) {
	svc, _ := setup(t)
	exp := svc.ForUser(alice)
	ctx := context.Background()
	if _, err := exp.Add(ctx, service.CreateInput{Amount: 120000, Category: domain.CategoryTransport, Comment: "такси"}); err != nil {
		t.Fatal(err)
	}
	last, err := exp.Add(ctx, service.CreateInput{Amount: 35000, Category: domain.CategoryFood, Comment: "кофе"})
	if err != nil {
		t.Fatal(err)
	}

	model := &scriptedModel{replies: []llm.Message{
		call("c1", "find_expenses", `{"sort": "created_at", "limit": 1}`),
		call("c2", "update_expense", fmt.Sprintf(`{"id": %d, "amount": 450}`, last.ID)),
		text("Исправил: кофе теперь 450 ₽."),
	}}
	if _, _, err := newAgent(model, svc).Reply(ctx, alice, nil, "последнее было не 350, а 450"); err != nil {
		t.Fatal(err)
	}

	found := model.seen[1][len(model.seen[1])-1].Content
	if !strings.Contains(found, fmt.Sprintf(`"id":%d`, last.ID)) {
		t.Fatalf("find_expenses returned %s, want last expense %d", found, last.ID)
	}
	got, err := svc.Get(ctx, last.ID, nil)
	if err != nil || got.Amount != 45000 {
		t.Fatalf("expense after update: %+v, %v", got, err)
	}
}

func TestValidationErrorGoesBackToModel(t *testing.T) {
	svc, _ := setup(t)
	model := &scriptedModel{replies: []llm.Message{
		call("c1", "add_expense", `{"amount": 100, "category": "food", "spent_at": "2030-01-01"}`),
		text("Нельзя записать трату на будущую дату."),
	}}
	if _, _, err := newAgent(model, svc).Reply(context.Background(), alice, nil, "обед 100 в 2030 году"); err != nil {
		t.Fatal(err)
	}
	toolResult := model.seen[1][len(model.seen[1])-1].Content
	if !strings.Contains(toolResult, `"error"`) {
		t.Fatalf("tool result = %s, want error", toolResult)
	}
}

func TestStepLimit(t *testing.T) {
	svc, _ := setup(t)
	model := &scriptedModel{}
	for i := 0; i < DefaultMaxSteps+1; i++ {
		model.replies = append(model.replies, call(fmt.Sprint("c", i), "find_expenses", `{}`))
	}
	reply, turn, err := newAgent(model, svc).Reply(context.Background(), alice, nil, "что-то странное")
	if err != nil {
		t.Fatal(err)
	}
	if reply != FallbackReply || len(model.seen) != DefaultMaxSteps {
		t.Fatalf("reply = %q after %d calls", reply, len(model.seen))
	}
	if last := turn[len(turn)-1]; last.Role != "assistant" || len(last.ToolCalls) != 0 {
		t.Fatalf("turn must end with a plain assistant message, got %+v", last)
	}
}
