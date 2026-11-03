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

// OpenAI-compatible provider: Chat Completions with Structured Outputs. It
// implements the same Client as the Anthropic path, using only net/http so
// REMNANT keeps its small dependency set. A base_url lets it reach
// OpenAI-compatible endpoints (Azure OpenAI, local gateways) too.

const openAIDefaultBase = "https://api.openai.com/v1"

type openaiClient struct {
	http   *http.Client
	base   string
	key    string
	model  string
	effort string
	// retries beyond the first attempt (default 2); -1 disables.
	retries int
}

func newOpenAIClient(p Profile) *openaiClient {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = openAIDefaultBase
	}
	timeout := 5 * time.Minute
	if p.Options.TimeoutSeconds > 0 {
		timeout = time.Duration(p.Options.TimeoutSeconds) * time.Second
	}
	retries := 2
	if p.Options.MaxRetries != nil {
		retries = *p.Options.MaxRetries
	}
	return &openaiClient{http: &http.Client{Timeout: timeout}, base: base, key: p.APIKey, model: p.Model, effort: p.Effort, retries: retries}
}

// isReasoningModel reports whether the model takes a reasoning_effort
// parameter (the o-series and GPT-5 reasoning models). Sending it to a
// plain chat model is an error, so it is only set for these.
func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	return strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4") ||
		strings.HasPrefix(m, "gpt-5") || strings.HasPrefix(m, "gpt-o")
}

// openaiEffort maps REMNANT's effort to the three values OpenAI accepts.
func openaiEffort(effort string) string {
	switch effort {
	case "low":
		return "low"
	case "medium":
		return "medium"
	default: // high, xhigh, max
		return "high"
	}
}

func (c *openaiClient) CompleteJSON(ctx context.Context, req Request) (Response, error) {
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.Input},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "remnant_response",
				"strict": true,
				"schema": req.Schema,
			},
		},
	}
	if req.MaxTokens > 0 {
		body["max_completion_tokens"] = req.MaxTokens
	}
	if isReasoningModel(c.model) {
		body["reasoning_effort"] = openaiEffort(c.effort)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}

	var resp Response
	var apiErr error
	for attempt := 0; attempt <= max(0, c.retries); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return resp, ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		resp, apiErr = c.once(ctx, raw)
		var re *retryable
		if apiErr == nil || !errors.As(apiErr, &re) {
			return resp, apiErr
		}
	}
	return resp, apiErr
}

// retryable marks a transient failure (429 or 5xx) worth another attempt.
type retryable struct{ error }

func (c *openaiClient) once(ctx context.Context, raw []byte) (Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, &retryable{err}
	}
	defer httpResp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if httpResp.StatusCode != http.StatusOK {
		msg := openAIErrorMessage(data)
		e := fmt.Errorf("openai: %d %s", httpResp.StatusCode, msg)
		if httpResp.StatusCode == http.StatusTooManyRequests || httpResp.StatusCode >= 500 {
			return Response{}, &retryable{e}
		}
		return Response{}, e
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Response{}, fmt.Errorf("openai: cannot parse response: %w", err)
	}
	out := Response{InputTokens: parsed.Usage.PromptTokens, OutputTokens: parsed.Usage.CompletionTokens}
	if len(parsed.Choices) == 0 {
		return out, errors.New("openai: response has no choices")
	}
	ch := parsed.Choices[0]
	if ch.Message.Refusal != "" {
		return out, &RefusalError{Category: "refusal"}
	}
	if ch.FinishReason == "length" {
		return out, errors.New("openai: response truncated at max_completion_tokens")
	}
	if !json.Valid([]byte(ch.Message.Content)) {
		return out, errors.New("openai: response is not valid JSON")
	}
	out.JSON = json.RawMessage(ch.Message.Content)
	return out, nil
}

func openAIErrorMessage(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return strings.TrimSpace(string(data))
}

// openaiCheck verifies the key, endpoint and model without spending tokens
// by looking the model up.
func openaiCheck(ctx context.Context, p Profile) (string, error) {
	c := newOpenAIClient(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models/"+c.model, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach the provider: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		return c.model, nil
	case http.StatusUnauthorized:
		return "", errors.New("the API key was rejected (401)")
	case http.StatusForbidden:
		return "", errors.New("the API key is not allowed to use this model (403)")
	case http.StatusNotFound:
		return "", fmt.Errorf("model %q was not found (404)", c.model)
	default:
		return "", fmt.Errorf("the provider answered %d: %s", resp.StatusCode, openAIErrorMessage(data))
	}
}
