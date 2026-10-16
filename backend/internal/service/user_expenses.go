package service

import (
	"context"

	"github.com/ivanmorozov/family-expenses/internal/domain"
)

// UserExpenses даёт доступ только к тратам одного пользователя. Его получает ИИ-агент бота.
// Владелец задаётся при создании и дальше не меняется, поэтому через этот тип нельзя
// прочитать или изменить чужую трату, какие бы аргументы ни передала модель.
type UserExpenses struct {
	svc    *Service
	userID int64
}

func (s *Service) ForUser(userID int64) *UserExpenses {
	return &UserExpenses{svc: s, userID: userID}
}

func (u *UserExpenses) UserID() int64 {
	return u.userID
}

func (u *UserExpenses) Add(ctx context.Context, in CreateInput) (domain.Expense, error) {
	in.UserID = u.userID
	return u.svc.Create(ctx, in)
}

func (u *UserExpenses) Find(ctx context.Context, f domain.ExpenseFilter) (ListResult, error) {
	f.UserID = &u.userID
	return u.svc.List(ctx, f)
}

func (u *UserExpenses) Summarize(ctx context.Context, f domain.ExpenseFilter) (Summary, error) {
	f.UserID = &u.userID
	return u.svc.Summarize(ctx, f)
}

func (u *UserExpenses) Update(ctx context.Context, id int64, p domain.ExpensePatch) (domain.Expense, error) {
	return u.svc.Update(ctx, id, &u.userID, p)
}

func (u *UserExpenses) Delete(ctx context.Context, id int64) error {
	return u.svc.Delete(ctx, id, &u.userID)
}
