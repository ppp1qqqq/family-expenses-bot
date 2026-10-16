// Package memstore хранит данные в памяти. Используется в тестах вместо PostgreSQL.
package memstore

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/domain"
)

type Store struct {
	mu       sync.Mutex
	nextID   int64
	users    map[int64]domain.User
	expenses map[int64]domain.Expense
}

func New() *Store {
	return &Store{users: map[int64]domain.User{}, expenses: map[int64]domain.Expense{}}
}

func (s *Store) UpsertUser(_ context.Context, u domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.users[u.TelegramID]; ok {
		u.CreatedAt = old.CreatedAt
	} else {
		u.CreatedAt = time.Now()
	}
	s.users[u.TelegramID] = u
	return nil
}

func (s *Store) ListUsers(context.Context) ([]domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TelegramID < out[j].TelegramID })
	return out, nil
}

func (s *Store) CreateExpense(_ context.Context, e domain.Expense) (domain.Expense, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[e.UserID]; !ok {
		return domain.Expense{}, domain.Invalid("пользователь %d не найден", e.UserID)
	}
	s.nextID++
	e.ID = s.nextID
	// Добавляем ID в наносекундах, чтобы порядок по created_at был строгим даже в быстрых тестах.
	e.CreatedAt = time.Now().Add(time.Duration(e.ID))
	e.UpdatedAt = e.CreatedAt
	s.expenses[e.ID] = e
	return e, nil
}

func (s *Store) get(id int64, owner *int64) (domain.Expense, error) {
	e, ok := s.expenses[id]
	if !ok || (owner != nil && e.UserID != *owner) {
		return domain.Expense{}, domain.ErrNotFound
	}
	return e, nil
}

func (s *Store) GetExpense(_ context.Context, id int64, owner *int64) (domain.Expense, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id, owner)
}

func (s *Store) UpdateExpense(_ context.Context, id int64, owner *int64, p domain.ExpensePatch) (domain.Expense, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.get(id, owner)
	if err != nil {
		return e, err
	}
	if p.Amount != nil {
		e.Amount = *p.Amount
	}
	if p.Category != nil {
		e.Category = *p.Category
	}
	if p.SpentAt != nil {
		e.SpentAt = *p.SpentAt
	}
	if p.Comment != nil {
		e.Comment = *p.Comment
	}
	e.UpdatedAt = time.Now()
	s.expenses[id] = e
	return e, nil
}

func (s *Store) DeleteExpense(_ context.Context, id int64, owner *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(id, owner); err != nil {
		return err
	}
	delete(s.expenses, id)
	return nil
}

func match(e domain.Expense, f domain.ExpenseFilter) bool {
	switch {
	case f.UserID != nil && e.UserID != *f.UserID:
		return false
	case f.From != nil && e.SpentAt.Before(*f.From):
		return false
	case f.To != nil && e.SpentAt.After(*f.To):
		return false
	case f.Category != nil && e.Category != *f.Category:
		return false
	case f.Text != "" && !strings.Contains(strings.ToLower(e.Comment), strings.ToLower(f.Text)):
		return false
	}
	return true
}

func (s *Store) filtered(f domain.ExpenseFilter) []domain.Expense {
	var out []domain.Expense
	for _, e := range s.expenses {
		if match(e, f) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if f.Sort != domain.SortCreatedAt && !a.SpentAt.Equal(b.SpentAt) {
			return a.SpentAt.After(b.SpentAt)
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
	return out
}

func (s *Store) ListExpenses(_ context.Context, f domain.ExpenseFilter) ([]domain.Expense, int, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.filtered(f)
	var sum int64
	for _, e := range all {
		sum += e.Amount
	}
	start := min(f.Offset, len(all))
	end := len(all)
	if f.Limit > 0 {
		end = min(start+f.Limit, len(all))
	}
	return all[start:end], len(all), sum, nil
}

func (s *Store) SumByCategory(_ context.Context, f domain.ExpenseFilter) ([]domain.CategoryTotal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	totals := map[domain.Category]*domain.CategoryTotal{}
	for _, e := range s.filtered(f) {
		t, ok := totals[e.Category]
		if !ok {
			t = &domain.CategoryTotal{Category: e.Category}
			totals[e.Category] = t
		}
		t.Amount += e.Amount
		t.Count++
	}
	out := make([]domain.CategoryTotal, 0, len(totals))
	for _, t := range totals {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	return out, nil
}
