// Package postgres хранит траты и пользователей в PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanmorozov/family-expenses/internal/domain"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

// Сумма хранится в NUMERIC(12,2), в Go живёт в копейках. Перевод делается в SQL.
const expenseColumns = `id, user_id, (amount * 100)::bigint, category, spent_at, comment, created_at, updated_at`

func scanExpense(row pgx.Row) (domain.Expense, error) {
	var e domain.Expense
	var category string
	err := row.Scan(&e.ID, &e.UserID, &e.Amount, &category, &e.SpentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt)
	e.Category = domain.Category(category)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, domain.ErrNotFound
	}
	return e, err
}

func (s *Store) UpsertUser(ctx context.Context, u domain.User) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (telegram_id, first_name, username) VALUES ($1, $2, $3)
		ON CONFLICT (telegram_id) DO UPDATE SET first_name = EXCLUDED.first_name, username = EXCLUDED.username`,
		u.TelegramID, u.FirstName, u.Username)
	return err
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT telegram_id, first_name, username, created_at FROM users ORDER BY first_name, telegram_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.User, error) {
		var u domain.User
		err := row.Scan(&u.TelegramID, &u.FirstName, &u.Username, &u.CreatedAt)
		return u, err
	})
}

func (s *Store) CreateExpense(ctx context.Context, e domain.Expense) (domain.Expense, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO expenses (user_id, amount, category, spent_at, comment)
		VALUES ($1, $2::numeric / 100, $3, $4, $5)
		RETURNING `+expenseColumns,
		e.UserID, e.Amount, string(e.Category), e.SpentAt, e.Comment)
	created, err := scanExpense(row)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return created, domain.Invalid("пользователь %d не найден", e.UserID)
	}
	return created, err
}

// ownerCond ограничивает запрос тратами владельца. owner == nil снимает ограничение (веб-панель).
func ownerCond(owner *int64) string {
	if owner == nil {
		return "TRUE"
	}
	return "user_id = $2"
}

func ownerArgs(id int64, owner *int64) []any {
	if owner == nil {
		return []any{id}
	}
	return []any{id, *owner}
}

func (s *Store) GetExpense(ctx context.Context, id int64, owner *int64) (domain.Expense, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+expenseColumns+` FROM expenses WHERE id = $1 AND `+ownerCond(owner),
		ownerArgs(id, owner)...)
	return scanExpense(row)
}

func (s *Store) UpdateExpense(ctx context.Context, id int64, owner *int64, p domain.ExpensePatch) (domain.Expense, error) {
	args := ownerArgs(id, owner)
	set := []string{"updated_at = now()"}
	add := func(expr string, v any) {
		args = append(args, v)
		set = append(set, fmt.Sprintf(expr, len(args)))
	}
	if p.Amount != nil {
		add("amount = $%d::numeric / 100", *p.Amount)
	}
	if p.Category != nil {
		add("category = $%d", string(*p.Category))
	}
	if p.SpentAt != nil {
		add("spent_at = $%d", *p.SpentAt)
	}
	if p.Comment != nil {
		add("comment = $%d", *p.Comment)
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE expenses SET `+strings.Join(set, ", ")+` WHERE id = $1 AND `+ownerCond(owner)+` RETURNING `+expenseColumns,
		args...)
	return scanExpense(row)
}

func (s *Store) DeleteExpense(ctx context.Context, id int64, owner *int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1 AND `+ownerCond(owner), ownerArgs(id, owner)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func buildWhere(f domain.ExpenseFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(expr string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(expr, len(args)))
	}
	if f.UserID != nil {
		add("user_id = $%d", *f.UserID)
	}
	if f.From != nil {
		add("spent_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("spent_at <= $%d", *f.To)
	}
	if f.Category != nil {
		add("category = $%d", string(*f.Category))
	}
	if f.Text != "" {
		add("position(lower($%d) in lower(comment)) > 0", f.Text)
	}
	if len(conds) == 0 {
		return "TRUE", args
	}
	return strings.Join(conds, " AND "), args
}

// ListExpenses возвращает страницу трат, а также общее число и сумму по всему фильтру.
func (s *Store) ListExpenses(ctx context.Context, f domain.ExpenseFilter) ([]domain.Expense, int, int64, error) {
	where, args := buildWhere(f)

	var total int
	var sum int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*), coalesce((sum(amount) * 100)::bigint, 0) FROM expenses WHERE `+where, args...,
	).Scan(&total, &sum); err != nil {
		return nil, 0, 0, err
	}

	order := "spent_at DESC, created_at DESC"
	if f.Sort == domain.SortCreatedAt {
		order = "created_at DESC"
	}
	query := fmt.Sprintf(`SELECT %s FROM expenses WHERE %s ORDER BY %s, id DESC LIMIT %d OFFSET %d`,
		expenseColumns, where, order, f.Limit, f.Offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Expense, error) {
		return scanExpense(row)
	})
	return items, total, sum, err
}

func (s *Store) SumByCategory(ctx context.Context, f domain.ExpenseFilter) ([]domain.CategoryTotal, error) {
	where, args := buildWhere(f)
	rows, err := s.pool.Query(ctx, `
		SELECT category, (sum(amount) * 100)::bigint, count(*)
		FROM expenses WHERE `+where+`
		GROUP BY category ORDER BY 2 DESC`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.CategoryTotal, error) {
		var t domain.CategoryTotal
		var category string
		err := row.Scan(&category, &t.Amount, &t.Count)
		t.Category = domain.Category(category)
		return t, err
	})
}
