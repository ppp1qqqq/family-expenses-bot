// Package config читает настройки из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
	Location    *time.Location
	LogLevel    string

	TelegramToken      string
	TelegramAllowedIDs []int64

	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
	LLMTimeout     time.Duration
	LLMTemperature float64
}

func (c Config) BotEnabled() bool {
	return c.TelegramToken != ""
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		LogLevel:      env("LOG_LEVEL", "info"),
		TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		LLMBaseURL:    env("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:     os.Getenv("LLM_API_KEY"),
		LLMModel:      os.Getenv("LLM_MODEL"),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}

	loc, err := time.LoadLocation(env("APP_TIMEZONE", "Europe/Moscow"))
	if err != nil {
		errs = append(errs, fmt.Errorf("APP_TIMEZONE: %w", err))
	}
	c.Location = loc

	if c.LLMTimeout, err = time.ParseDuration(env("LLM_TIMEOUT", "30s")); err != nil {
		errs = append(errs, fmt.Errorf("LLM_TIMEOUT: %w", err))
	}
	if c.LLMTemperature, err = strconv.ParseFloat(env("LLM_TEMPERATURE", "0.2"), 64); err != nil {
		errs = append(errs, fmt.Errorf("LLM_TEMPERATURE: %w", err))
	}

	for _, s := range strings.Split(os.Getenv("TELEGRAM_ALLOWED_IDS"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("TELEGRAM_ALLOWED_IDS: bad id %q", s))
			continue
		}
		c.TelegramAllowedIDs = append(c.TelegramAllowedIDs, id)
	}

	if c.BotEnabled() && c.LLMModel == "" {
		errs = append(errs, errors.New("LLM_MODEL is required when TELEGRAM_BOT_TOKEN is set"))
	}
	return c, errors.Join(errs...)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
