package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxBody = 1 << 20

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	MaxTokens      int            `json:"max_tokens"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat map[string]any `json:"response_format"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (a *Adapter) complete(ctx context.Context, user string, schema map[string]any) ([]byte, error) {
	payload := chatRequest{
		Model:       a.cfg.Model,
		MaxTokens:   a.cfg.MaxTokens,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: user},
		},
		ResponseFormat: map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "context_events",
				"strict": true,
				"schema": schema,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBody {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai chat completions HTTP %d: %s", resp.StatusCode, excerpt(raw))
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("chat response has no choices")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("chat response has empty content")
	}
	return []byte(content), nil
}
