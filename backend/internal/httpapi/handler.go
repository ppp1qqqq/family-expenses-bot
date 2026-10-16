// Package httpapi реализует REST API для веб-панели главы семьи. Авторизации нет по условию задания.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/service"
)

type Handler struct {
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) http.Handler {
	h := &Handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/expenses", h.listExpenses)
	mux.HandleFunc("POST /api/v1/expenses", h.createExpense)
	mux.HandleFunc("GET /api/v1/expenses/{id}", h.getExpense)
	mux.HandleFunc("PATCH /api/v1/expenses/{id}", h.updateExpense)
	mux.HandleFunc("DELETE /api/v1/expenses/{id}", h.deleteExpense)
	mux.HandleFunc("GET /api/v1/users", h.listUsers)
	mux.HandleFunc("GET /api/v1/categories", h.listCategories)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return h.recoverer(mux)
}

type expenseJSON struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"user_id"`
	Amount    json.Number `json:"amount"`
	Category  string      `json:"category"`
	SpentAt   string      `json:"spent_at"`
	Comment   string      `json:"comment"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func toJSON(e domain.Expense) expenseJSON {
	return expenseJSON{
		ID:        e.ID,
		UserID:    e.UserID,
		Amount:    json.Number(domain.FormatAmount(e.Amount)),
		Category:  string(e.Category),
		SpentAt:   e.SpentAt.Format(domain.DateLayout),
		Comment:   e.Comment,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

type userJSON struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type categoryJSON struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (h *Handler) listExpenses(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	res, err := h.svc.List(r.Context(), f)
	if err != nil {
		h.fail(w, err)
		return
	}
	items := make([]expenseJSON, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, toJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": res.Total,
		"sum":   json.Number(domain.FormatAmount(res.Sum)),
	})
}

func parseFilter(r *http.Request) (domain.ExpenseFilter, error) {
	q := r.URL.Query()
	var f domain.ExpenseFilter
	if v := q.Get("user_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return f, domain.Invalid("некорректный user_id")
		}
		f.UserID = &id
	}
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if v := q.Get(p.name); v != "" {
			d, err := domain.ParseDate(v)
			if err != nil {
				return f, err
			}
			*p.dst = &d
		}
	}
	if v := q.Get("category"); v != "" {
		c := domain.Category(v)
		f.Category = &c
	}
	f.Text = q.Get("q")
	var err error
	if f.Limit, err = intParam(q.Get("limit")); err != nil {
		return f, domain.Invalid("некорректный limit")
	}
	if f.Offset, err = intParam(q.Get("offset")); err != nil {
		return f, domain.Invalid("некорректный offset")
	}
	return f, nil
}

func intParam(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}

type createRequest struct {
	UserID   int64       `json:"user_id"`
	Amount   json.Number `json:"amount"`
	Category string      `json:"category"`
	SpentAt  string      `json:"spent_at"`
	Comment  string      `json:"comment"`
}

func (h *Handler) createExpense(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, err)
		return
	}
	if req.UserID == 0 {
		h.fail(w, domain.Invalid("не указан член семьи"))
		return
	}
	amount, err := domain.ParseAmount(req.Amount.String())
	if err != nil {
		h.fail(w, err)
		return
	}
	in := service.CreateInput{
		UserID:   req.UserID,
		Amount:   amount,
		Category: domain.Category(req.Category),
		Comment:  req.Comment,
	}
	if req.SpentAt != "" {
		d, err := domain.ParseDate(req.SpentAt)
		if err != nil {
			h.fail(w, err)
			return
		}
		in.SpentAt = &d
	}
	e, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toJSON(e))
}

func (h *Handler) getExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	e, err := h.svc.Get(r.Context(), id, nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(e))
}

type updateRequest struct {
	Amount   *json.Number `json:"amount"`
	Category *string      `json:"category"`
	SpentAt  *string      `json:"spent_at"`
	Comment  *string      `json:"comment"`
}

func (h *Handler) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var req updateRequest
	if err := decode(r, &req); err != nil {
		h.fail(w, err)
		return
	}
	var p domain.ExpensePatch
	if req.Amount != nil {
		a, err := domain.ParseAmount(req.Amount.String())
		if err != nil {
			h.fail(w, err)
			return
		}
		p.Amount = &a
	}
	if req.Category != nil {
		c := domain.Category(*req.Category)
		p.Category = &c
	}
	if req.SpentAt != nil {
		d, err := domain.ParseDate(*req.SpentAt)
		if err != nil {
			h.fail(w, err)
			return
		}
		p.SpentAt = &d
	}
	p.Comment = req.Comment
	e, err := h.svc.Update(r.Context(), id, nil, p)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(e))
}

func (h *Handler) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id, nil); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.Users(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]userJSON, 0, len(users))
	for _, u := range users {
		out = append(out, userJSON{ID: u.TelegramID, FirstName: u.FirstName, Username: u.Username})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) listCategories(w http.ResponseWriter, _ *http.Request) {
	out := make([]categoryJSON, 0, len(domain.Categories))
	for _, c := range domain.Categories {
		out = append(out, categoryJSON{Code: string(c), Name: c.Name()})
	}
	writeJSON(w, http.StatusOK, out)
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, domain.Invalid("некорректный id")
	}
	return id, nil
}

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.Invalid("некорректный JSON: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	var body errorBody
	status := http.StatusInternalServerError
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		status, body.Error.Code, body.Error.Message = http.StatusBadRequest, "validation", verr.Message
	case errors.Is(err, domain.ErrNotFound):
		status, body.Error.Code, body.Error.Message = http.StatusNotFound, "not_found", "трата не найдена"
	default:
		h.log.Error("request failed", "err", err)
		body.Error.Code, body.Error.Message = "internal", "внутренняя ошибка"
	}
	writeJSON(w, status, body)
}

func (h *Handler) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				h.log.Error("panic in handler", "panic", rec, "path", r.URL.Path)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
