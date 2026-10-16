// Package service содержит правила работы с тратами. Им пользуются и REST API, и ИИ-агент бота.
package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivanmorozov/family-expenses/internal/domain"
)

// Repository реализуют storage/postgres и storage/memstore.
// owner == nil снимает ограничение по владельцу, так работает только веб-панель.
type Repository interface {
	UpsertUser(ctx context.Context, u domain.User) error
	ListUsers(ctx context.Context) ([]domain.User, error)
	CreateExpense(ctx context.Context, e domain.Expense) (domain.Expense, error)
	GetExpense(ctx context.Context, id int64, owner *int64) (domain.Expense, error)
	UpdateExpense(ctx context.Context, id int64, owner *int64, p domain.ExpensePatch) (domain.Expense, error)
	DeleteExpense(ctx context.Context, id int64, owner *int64) error
	ListExpenses(ctx context.Context, f domain.ExpenseFilter) ([]domain.Expense, int, int64, error)
	SumByCategory(ctx context.Context, f domain.ExpenseFilter) ([]domain.CategoryTotal, error)
}

const (
	DefaultLimit = 50
	MaxLimit     = 500

	maxAmount        = 100_000_000_00 // 100 млн рублей в копейках
	maxCommentLength = 500
)

type Service struct {
	repo Repository
	loc  *time.Location
	now  func() time.Time
}

func New(repo Repository, loc *time.Location) *Service {
	return &Service{repo: repo, loc: loc, now: time.Now}
}

// WithClock подменяет часы, нужен в тестах.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Today возвращает сегодняшнюю дату в часовом поясе сервиса (полночь UTC, как хранится DATE).
func (s *Service) Today() time.Time {
	y, m, d := s.now().In(s.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (s *Service) Location() *time.Location {
	return s.loc
}

type CreateInput struct {
	UserID   int64
	Amount   int64
	Category domain.Category
	SpentAt  *time.Time // nil означает сегодня
	Comment  string
}

type ListResult struct {
	Items []domain.Expense
	Total int
	Sum   int64
}

type Summary struct {
	Total      int64
	Count      int
	ByCategory []domain.CategoryTotal
}

func (s *Service) RegisterUser(ctx context.Context, u domain.User) error {
	if strings.TrimSpace(u.FirstName) == "" {
		u.FirstName = u.Username
	}
	return s.repo.UpsertUser(ctx, u)
}

func (s *Service) Users(ctx context.Context) ([]domain.User, error) {
	return s.repo.ListUsers(ctx)
}

func (s *Service) Create(ctx context.Context, in CreateInput) (domain.Expense, error) {
	spentAt := s.Today()
	if in.SpentAt != nil {
		spentAt = *in.SpentAt
	}
	comment := strings.TrimSpace(in.Comment)
	if err := s.validate(in.Amount, in.Category, spentAt, comment); err != nil {
		return domain.Expense{}, err
	}
	return s.repo.CreateExpense(ctx, domain.Expense{
		UserID:   in.UserID,
		Amount:   in.Amount,
		Category: in.Category,
		SpentAt:  spentAt,
		Comment:  comment,
	})
}

func (s *Service) Get(ctx context.Context, id int64, owner *int64) (domain.Expense, error) {
	return s.repo.GetExpense(ctx, id, owner)
}

func (s *Service) Update(ctx context.Context, id int64, owner *int64, p domain.ExpensePatch) (domain.Expense, error) {
	if p.Empty() {
		return domain.Expense{}, domain.Invalid("нечего изменять")
	}
	if p.Comment != nil {
		c := strings.TrimSpace(*p.Comment)
		p.Comment = &c
	}
	cur, err := s.repo.GetExpense(ctx, id, owner)
	if err != nil {
		return domain.Expense{}, err
	}
	amount, category, spentAt, comment := cur.Amount, cur.Category, cur.SpentAt, cur.Comment
	if p.Amount != nil {
		amount = *p.Amount
	}
	if p.Category != nil {
		category = *p.Category
	}
	if p.SpentAt != nil {
		spentAt = *p.SpentAt
	}
	if p.Comment != nil {
		comment = *p.Comment
	}
	if err := s.validate(amount, category, spentAt, comment); err != nil {
		return domain.Expense{}, err
	}
	return s.repo.UpdateExpense(ctx, id, owner, p)
}

func (s *Service) Delete(ctx context.Context, id int64, owner *int64) error {
	return s.repo.DeleteExpense(ctx, id, owner)
}

func (s *Service) List(ctx context.Context, f domain.ExpenseFilter) (ListResult, error) {
	if err := s.normalizeFilter(&f); err != nil {
		return ListResult{}, err
	}
	items, total, sum, err := s.repo.ListExpenses(ctx, f)
	if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total, Sum: sum}, nil
}

func (s *Service) Summarize(ctx context.Context, f domain.ExpenseFilter) (Summary, error) {
	if err := s.normalizeFilter(&f); err != nil {
		return Summary{}, err
	}
	byCategory, err := s.repo.SumByCategory(ctx, f)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{ByCategory: byCategory}
	for _, t := range byCategory {
		sum.Total += t.Amount
		sum.Count += t.Count
	}
	return sum, nil
}

func (s *Service) validate(amount int64, c domain.Category, spentAt time.Time, comment string) error {
	switch {
	case amount <= 0:
		return domain.Invalid("сумма должна быть больше нуля")
	case amount > maxAmount:
		return domain.Invalid("сумма слишком большая")
	case !c.Valid():
		return domain.Invalid("неизвестная категория %q", c)
	case spentAt.After(s.Today()):
		return domain.Invalid("дата траты %s ещё не наступила", spentAt.Format(domain.DateLayout))
	case utf8.RuneCountInString(comment) > maxCommentLength:
		return domain.Invalid("комментарий длиннее %d символов", maxCommentLength)
	}
	return nil
}

func (s *Service) normalizeFilter(f *domain.ExpenseFilter) error {
	if f.Category != nil && !f.Category.Valid() {
		return domain.Invalid("неизвестная категория %q", *f.Category)
	}
	if f.From != nil && f.To != nil && f.From.After(*f.To) {
		return domain.Invalid("начало периода позже конца")
	}
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	if f.Limit > MaxLimit {
		f.Limit = MaxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	f.Text = strings.TrimSpace(f.Text)
	return nil
}
