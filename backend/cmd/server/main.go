// Сервер учёта семейных расходов: REST API для веб-панели и Telegram-бот с ИИ-агентом в одном процессе.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // часовые пояса нужны и в минимальном образе без tzdata

	"github.com/ivanmorozov/family-expenses/internal/agent"
	"github.com/ivanmorozov/family-expenses/internal/bot"
	"github.com/ivanmorozov/family-expenses/internal/config"
	"github.com/ivanmorozov/family-expenses/internal/httpapi"
	"github.com/ivanmorozov/family-expenses/internal/llm"
	"github.com/ivanmorozov/family-expenses/internal/service"
	"github.com/ivanmorozov/family-expenses/internal/storage/postgres"
	"github.com/ivanmorozov/family-expenses/internal/telegram"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}

	svc := service.New(store, cfg.Location)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(svc, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("http server started", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if cfg.BotEnabled() {
		if len(cfg.TelegramAllowedIDs) == 0 {
			log.Warn("TELEGRAM_ALLOWED_IDS is empty: the bot answers anyone who writes to it")
		}
		model := llm.New(llm.Config{
			BaseURL:     cfg.LLMBaseURL,
			APIKey:      cfg.LLMAPIKey,
			Model:       cfg.LLMModel,
			Timeout:     cfg.LLMTimeout,
			Temperature: cfg.LLMTemperature,
		})
		b := bot.New(telegram.New(cfg.TelegramToken), agent.New(model, svc, log), svc, cfg.TelegramAllowedIDs, log)
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("telegram bot started", "llm_model", cfg.LLMModel, "llm_base_url", cfg.LLMBaseURL)
			b.Run(ctx)
		}()
	} else {
		log.Warn("TELEGRAM_BOT_TOKEN is empty: the bot is disabled, only the REST API is running")
	}

	select {
	case <-ctx.Done():
	case err = <-errCh:
		stop()
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	wg.Wait()
	return err
}

// connect ждёт базу до минуты: при первом запуске compose PostgreSQL может ещё инициализироваться.
func connect(ctx context.Context, dsn string, log *slog.Logger) (*postgres.Store, error) {
	deadline := time.Now().Add(time.Minute)
	for {
		store, err := postgres.New(ctx, dsn)
		if err == nil {
			return store, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, err
		}
		log.Info("waiting for postgres", "err", err)
		time.Sleep(2 * time.Second)
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
