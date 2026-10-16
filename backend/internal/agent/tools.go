package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/llm"
	"github.com/ivanmorozov/family-expenses/internal/service"
)

// В схемах нет user_id: владельца задаёт сервер через service.UserExpenses.

const categoryEnum = `["food", "transport", "housing", "entertainment", "health", "shopping", "other"]`

var tools = []llm.Tool{
	tool("add_expense", "Добавить новую трату пользователя.", `{
		"type": "object",
		"properties": {
			"amount":   {"type": "number", "description": "Сумма в рублях, больше нуля"},
			"category": {"type": "string", "enum": `+categoryEnum+`},
			"spent_at": {"type": "string", "description": "Дата траты YYYY-MM-DD. Если не указана, сегодня"},
			"comment":  {"type": "string", "description": "На что потрачено, коротко: кофе, такси, аптека"}
		},
		"required": ["amount", "category"]
	}`),
	tool("find_expenses", "Найти траты пользователя. Используй и для показа списка, и чтобы узнать id перед изменением или удалением.", `{
		"type": "object",
		"properties": {
			"from":     {"type": "string", "description": "Начало периода YYYY-MM-DD включительно"},
			"to":       {"type": "string", "description": "Конец периода YYYY-MM-DD включительно"},
			"category": {"type": "string", "enum": `+categoryEnum+`},
			"text":     {"type": "string", "description": "Подстрока комментария, например такси"},
			"sort":     {"type": "string", "enum": ["spent_at", "created_at"], "description": "spent_at: по дате траты; created_at: последние добавленные сверху"},
			"limit":    {"type": "integer", "description": "Сколько вернуть, по умолчанию 10, максимум 50"}
		}
	}`),
	tool("sum_expenses", "Посчитать сумму трат пользователя за период, всего и по категориям.", `{
		"type": "object",
		"properties": {
			"from":     {"type": "string", "description": "Начало периода YYYY-MM-DD включительно"},
			"to":       {"type": "string", "description": "Конец периода YYYY-MM-DD включительно"},
			"category": {"type": "string", "enum": `+categoryEnum+`}
		}
	}`),
	tool("update_expense", "Изменить трату пользователя по id. Передавай только изменяемые поля.", `{
		"type": "object",
		"properties": {
			"id":       {"type": "integer"},
			"amount":   {"type": "number"},
			"category": {"type": "string", "enum": `+categoryEnum+`},
			"spent_at": {"type": "string", "description": "YYYY-MM-DD"},
			"comment":  {"type": "string"}
		},
		"required": ["id"]
	}`),
	tool("delete_expense", "Удалить трату пользователя по id.", `{
		"type": "object",
		"properties": {"id": {"type": "integer"}},
		"required": ["id"]
	}`),
}

func tool(name, description, schema string) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.FunctionDef{
		Name:        name,
		Description: description,
		Parameters:  json.RawMessage(schema),
	}}
}

type expenseOut struct {
	ID       int64       `json:"id"`
	Amount   json.Number `json:"amount"`
	Category string      `json:"category"`
	SpentAt  string      `json:"spent_at"`
	Comment  string      `json:"comment,omitempty"`
}

func toOut(e domain.Expense) expenseOut {
	return expenseOut{
		ID:       e.ID,
		Amount:   json.Number(domain.FormatAmount(e.Amount)),
		Category: string(e.Category),
		SpentAt:  e.SpentAt.Format(domain.DateLayout),
		Comment:  e.Comment,
	}
}

type periodArgs struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Category string `json:"category"`
}

func (p periodArgs) apply(f *domain.ExpenseFilter) error {
	if p.From != "" {
		d, err := domain.ParseDate(p.From)
		if err != nil {
			return err
		}
		f.From = &d
	}
	if p.To != "" {
		d, err := domain.ParseDate(p.To)
		if err != nil {
			return err
		}
		f.To = &d
	}
	if p.Category != "" {
		c := domain.Category(p.Category)
		f.Category = &c
	}
	return nil
}

// execute выполняет вызов инструмента и возвращает JSON для модели.
// Ошибки валидации и «не найдено» отдаются модели текстом, чтобы она могла объяснить их пользователю.
func execute(ctx context.Context, exp *service.UserExpenses, call llm.FunctionCall) (string, error) {
	result, err := dispatch(ctx, exp, call)
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		return errorResult(verr.Message), nil
	case errors.Is(err, domain.ErrNotFound):
		return errorResult("трата не найдена"), nil
	case err != nil:
		return "", err
	}
	b, err := json.Marshal(result)
	return string(b), err
}

func errorResult(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

func decodeArgs(raw string, dst any) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return domain.Invalid("некорректные аргументы: %v", err)
	}
	return nil
}

func dispatch(ctx context.Context, exp *service.UserExpenses, call llm.FunctionCall) (any, error) {
	switch call.Name {
	case "add_expense":
		var a struct {
			Amount   json.Number `json:"amount"`
			Category string      `json:"category"`
			SpentAt  string      `json:"spent_at"`
			Comment  string      `json:"comment"`
		}
		if err := decodeArgs(call.Arguments, &a); err != nil {
			return nil, err
		}
		amount, err := domain.ParseAmount(a.Amount.String())
		if err != nil {
			return nil, err
		}
		in := service.CreateInput{Amount: amount, Category: domain.Category(a.Category), Comment: a.Comment}
		if a.SpentAt != "" {
			d, err := domain.ParseDate(a.SpentAt)
			if err != nil {
				return nil, err
			}
			in.SpentAt = &d
		}
		e, err := exp.Add(ctx, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"added": toOut(e)}, nil

	case "find_expenses":
		var a struct {
			periodArgs
			Text  string `json:"text"`
			Sort  string `json:"sort"`
			Limit int    `json:"limit"`
		}
		if err := decodeArgs(call.Arguments, &a); err != nil {
			return nil, err
		}
		f := domain.ExpenseFilter{Text: a.Text, Sort: domain.SortOrder(a.Sort), Limit: a.Limit}
		if f.Limit <= 0 {
			f.Limit = 10
		}
		f.Limit = min(f.Limit, 50)
		if err := a.apply(&f); err != nil {
			return nil, err
		}
		res, err := exp.Find(ctx, f)
		if err != nil {
			return nil, err
		}
		items := make([]expenseOut, 0, len(res.Items))
		for _, e := range res.Items {
			items = append(items, toOut(e))
		}
		return map[string]any{
			"items":       items,
			"total_found": res.Total,
			"total_sum":   json.Number(domain.FormatAmount(res.Sum)),
		}, nil

	case "sum_expenses":
		var a periodArgs
		if err := decodeArgs(call.Arguments, &a); err != nil {
			return nil, err
		}
		var f domain.ExpenseFilter
		if err := a.apply(&f); err != nil {
			return nil, err
		}
		sum, err := exp.Summarize(ctx, f)
		if err != nil {
			return nil, err
		}
		byCategory := make(map[string]json.Number, len(sum.ByCategory))
		for _, t := range sum.ByCategory {
			byCategory[string(t.Category)] = json.Number(domain.FormatAmount(t.Amount))
		}
		return map[string]any{
			"total":       json.Number(domain.FormatAmount(sum.Total)),
			"count":       sum.Count,
			"by_category": byCategory,
		}, nil

	case "update_expense":
		var a struct {
			ID       int64        `json:"id"`
			Amount   *json.Number `json:"amount"`
			Category *string      `json:"category"`
			SpentAt  *string      `json:"spent_at"`
			Comment  *string      `json:"comment"`
		}
		if err := decodeArgs(call.Arguments, &a); err != nil {
			return nil, err
		}
		var p domain.ExpensePatch
		if a.Amount != nil {
			v, err := domain.ParseAmount(a.Amount.String())
			if err != nil {
				return nil, err
			}
			p.Amount = &v
		}
		if a.Category != nil {
			c := domain.Category(*a.Category)
			p.Category = &c
		}
		if a.SpentAt != nil {
			d, err := domain.ParseDate(*a.SpentAt)
			if err != nil {
				return nil, err
			}
			p.SpentAt = &d
		}
		p.Comment = a.Comment
		e, err := exp.Update(ctx, a.ID, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"updated": toOut(e)}, nil

	case "delete_expense":
		var a struct {
			ID int64 `json:"id"`
		}
		if err := decodeArgs(call.Arguments, &a); err != nil {
			return nil, err
		}
		if err := exp.Delete(ctx, a.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted_id": a.ID}, nil
	}
	return nil, domain.Invalid("неизвестный инструмент %q", call.Name)
}

func describeCall(c llm.FunctionCall) string {
	return fmt.Sprintf("%s(%s)", c.Name, c.Arguments)
}
