package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/httpapi"
	"github.com/ivanmorozov/family-expenses/internal/service"
	"github.com/ivanmorozov/family-expenses/internal/storage/memstore"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := memstore.New()
	for _, id := range []int64{1, 2} {
		if err := store.UpsertUser(context.Background(), domain.User{TelegramID: id, FirstName: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	svc := service.New(store, time.UTC).WithClock(func() time.Time { return now })
	srv := httptest.NewServer(httpapi.New(svc, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestExpenseCRUD(t *testing.T) {
	srv := newServer(t)
	base := srv.URL + "/api/v1/expenses"

	status, created := do(t, "POST", base, `{"user_id": 1, "amount": 350.5, "category": "food", "comment": "кофе"}`)
	if status != http.StatusCreated || created["amount"] != 350.5 || created["spent_at"] != "2026-10-16" {
		t.Fatalf("create: %d %v", status, created)
	}
	id := int(created["id"].(float64))
	do(t, "POST", base, `{"user_id": 2, "amount": 1200, "category": "transport", "spent_at": "2026-10-15"}`)

	status, list := do(t, "GET", base+"?user_id=1", "")
	if status != http.StatusOK || list["total"] != 1.0 || list["sum"] != 350.5 {
		t.Fatalf("list by user: %d %v", status, list)
	}
	_, list = do(t, "GET", base+"?from=2026-10-15&to=2026-10-15&category=transport", "")
	if list["total"] != 1.0 {
		t.Fatalf("list by period and category: %v", list)
	}

	status, updated := do(t, "PATCH", base+"/"+itoa(id), `{"amount": 450, "comment": "латте"}`)
	if status != http.StatusOK || updated["amount"] != 450.0 || updated["comment"] != "латте" || updated["category"] != "food" {
		t.Fatalf("update: %d %v", status, updated)
	}

	if status, _ := do(t, "DELETE", base+"/"+itoa(id), ""); status != http.StatusNoContent {
		t.Fatalf("delete: %d", status)
	}
	if status, _ := do(t, "GET", base+"/"+itoa(id), ""); status != http.StatusNotFound {
		t.Fatalf("get after delete: %d", status)
	}
}

func TestValidationErrors(t *testing.T) {
	srv := newServer(t)
	base := srv.URL + "/api/v1/expenses"
	cases := []string{
		`{"user_id": 1, "amount": -5, "category": "food"}`,
		`{"user_id": 1, "amount": 100, "category": "coffee"}`,
		`{"user_id": 1, "amount": 100, "category": "food", "spent_at": "2030-01-01"}`,
		`{"user_id": 99, "amount": 100, "category": "food"}`,
		`{"amount": 100, "category": "food"}`,
		`{"user_id": 1, "amount": 100, "category": "food", "extra": 1}`,
	}
	for _, body := range cases {
		status, out := do(t, "POST", base, body)
		errObj, _ := out["error"].(map[string]any)
		if status != http.StatusBadRequest || errObj["code"] != "validation" {
			t.Errorf("POST %s: %d %v", body, status, out)
		}
	}
	if status, _ := do(t, "GET", base+"?from=yesterday", ""); status != http.StatusBadRequest {
		t.Errorf("bad date filter: %d", status)
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
