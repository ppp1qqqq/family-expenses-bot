package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/service"
	"github.com/ivanmorozov/family-expenses/internal/storage/memstore"
)

const (
	alice int64 = 1
	bob   int64 = 2
)

func newService(t *testing.T) *service.Service {
	t.Helper()
	store := memstore.New()
	ctx := context.Background()
	for _, u := range []domain.User{{TelegramID: alice, FirstName: "Алиса"}, {TelegramID: bob, FirstName: "Боб"}} {
		if err := store.UpsertUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	return service.New(store, time.UTC).WithClock(func() time.Time { return now })
}

func TestUserCannotTouchOthersExpenses(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	bobsExpense, err := svc.ForUser(bob).Add(ctx, service.CreateInput{Amount: 120000, Category: domain.CategoryTransport, Comment: "такси"})
	if err != nil {
		t.Fatal(err)
	}

	aliceExp := svc.ForUser(alice)
	newAmount := int64(1)
	if _, err := aliceExp.Update(ctx, bobsExpense.ID, domain.ExpensePatch{Amount: &newAmount}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update of foreign expense: got %v, want ErrNotFound", err)
	}
	if err := aliceExp.Delete(ctx, bobsExpense.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete of foreign expense: got %v, want ErrNotFound", err)
	}

	// Даже если в фильтре указан чужой user_id, UserExpenses его перезапишет.
	res, err := aliceExp.Find(ctx, domain.ExpenseFilter{UserID: ptr(bob)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Fatalf("alice sees %d foreign expenses", res.Total)
	}

	got, err := svc.Get(ctx, bobsExpense.ID, nil)
	if err != nil || got.Amount != 120000 {
		t.Fatalf("bob's expense changed: %+v, %v", got, err)
	}
}

func TestCreateDefaultsAndValidation(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	exp := svc.ForUser(alice)

	e, err := exp.Add(ctx, service.CreateInput{Amount: 35000, Category: domain.CategoryFood, Comment: "  кофе "})
	if err != nil {
		t.Fatal(err)
	}
	if e.SpentAt.Format(domain.DateLayout) != "2026-10-16" || e.Comment != "кофе" || e.UserID != alice {
		t.Fatalf("unexpected expense: %+v", e)
	}

	tomorrow := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	invalid := []service.CreateInput{
		{Amount: 0, Category: domain.CategoryFood},
		{Amount: 100, Category: "coffee"},
		{Amount: 100, Category: domain.CategoryFood, SpentAt: &tomorrow},
	}
	for _, in := range invalid {
		var verr *domain.ValidationError
		if _, err := exp.Add(ctx, in); !errors.As(err, &verr) {
			t.Errorf("Add(%+v): got %v, want validation error", in, err)
		}
	}
}

func TestSummarize(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	exp := svc.ForUser(alice)
	for _, in := range []service.CreateInput{
		{Amount: 35000, Category: domain.CategoryFood},
		{Amount: 15050, Category: domain.CategoryFood},
		{Amount: 120000, Category: domain.CategoryTransport},
	} {
		if _, err := exp.Add(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.ForUser(bob).Add(ctx, service.CreateInput{Amount: 999900, Category: domain.CategoryFood}); err != nil {
		t.Fatal(err)
	}

	food := domain.CategoryFood
	sum, err := exp.Summarize(ctx, domain.ExpenseFilter{Category: &food})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 50050 || sum.Count != 2 {
		t.Fatalf("food summary = %+v, want 50050 in 2 expenses", sum)
	}
}

func ptr[T any](v T) *T { return &v }
