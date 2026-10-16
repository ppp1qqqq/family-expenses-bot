// Package llm это клиент OpenAI-совместимого Chat Completions API с поддержкой вызова инструментов.
// Подходит для OpenAI, OpenRouter, DeepSeek, Ollama, vLLM и других провайдеров с таким API.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type Config struct {
	BaseURL     string
	APIKey      string
	Model       string
	Timeout     time.Duration
	Temperature float64
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat отправляет историю и возвращает ответ модели: текст или вызовы инструментов.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	body, err := json.Marshal(chatRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Tools:       tools,
		Temperature: c.cfg.Temperature,
	})
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Message{}, fmt.Errorf("llm read: %w", err)
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Message{}, fmt.Errorf("llm status %d: %s", resp.StatusCode, truncate(raw))
	}
	if out.Error != nil {
		return Message{}, fmt.Errorf("llm status %d: %s", resp.StatusCode, out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("llm status %d: %s", resp.StatusCode, truncate(raw))
	}
	if len(out.Choices) == 0 {
		return Message{}, errors.New("llm: empty choices")
	}
	msg := out.Choices[0].Message
	msg.Role = "assistant"
	return msg, nil
}

func truncate(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
