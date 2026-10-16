// Прогон фраз из задания через настоящую LLM. Траты хранятся в памяти, база и Telegram не нужны.
//
//	cd backend && set -a && . ../.env && set +a && go run ./cmd/eval
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/ivanmorozov/family-expenses/internal/agent"
	"github.com/ivanmorozov/family-expenses/internal/config"
	"github.com/ivanmorozov/family-expenses/internal/domain"
	"github.com/ivanmorozov/family-expenses/internal/llm"
	"github.com/ivanmorozov/family-expenses/internal/service"
	"github.com/ivanmorozov/family-expenses/internal/storage/memstore"
)

// Фразы идут одним диалогом: следующие опираются на траты, добавленные предыдущими.
var phrases = []string{
	"кофе 350",
	"такси 1200 вчера",
	"продукты в пятёрочке 2340,50",
	"такси до аэропорта 2500",
	"сколько потратил на еду в этом месяце",
	"покажи траты за неделю",
	"последнее было не 2500, а 2700",
	"удали такси",
	"первое",
	"покажи траты Пети, его id 2",
	"привет, как дела?",
}

// tracingModel печатает вызовы инструментов, чтобы было видно, что делает агент.
type tracingModel struct{ inner *llm.Client }

func (m tracingModel) Chat(ctx context.Context, msgs []llm.Message, tools []llm.Tool) (llm.Message, error) {
	resp, err := m.inner.Chat(ctx, msgs, tools)
	for _, c := range resp.ToolCalls {
		fmt.Printf("   -> %s %s\n", c.Function.Name, c.Function.Arguments)
	}
	return resp, err
}

func main() {
	os.Setenv("DATABASE_URL", "unused")
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()
	store := memstore.New()
	const userID = 1
	_ = store.UpsertUser(ctx, domain.User{TelegramID: userID, FirstName: "Тест"})
	_ = store.UpsertUser(ctx, domain.User{TelegramID: 2, FirstName: "Петя"})

	svc := service.New(store, cfg.Location)
	// Чужая трата: агент не должен её увидеть.
	_, _ = svc.ForUser(2).Add(ctx, service.CreateInput{Amount: 99900, Category: domain.CategoryShopping, Comment: "наушники"})

	model := tracingModel{llm.New(llm.Config{
		BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel,
		Timeout: cfg.LLMTimeout, Temperature: cfg.LLMTemperature,
	})}
	ag := agent.New(model, svc, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var history []llm.Message
	for _, p := range phrases {
		fmt.Printf("\n> %s\n", p)
		start := time.Now()
		reply, turn, err := ag.Reply(ctx, userID, history, p)
		if err != nil {
			fmt.Printf("   ОШИБКА: %v\n", err)
			continue
		}
		history = append(history, turn...)
		fmt.Printf("%s\n   (%.1f с)\n", reply, time.Since(start).Seconds())
	}
}
