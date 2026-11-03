// Package llm turns a stored provider profile into a client that returns
// schema-constrained JSON. Agents only ever see this interface.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Profile is a decrypted provider profile. It lives only in memory.
type Profile struct {
	ID       int64
	Provider string
	BaseURL  string
	Model    string
	Effort   string
	APIKey   string
	Options  Options
}

// Options are a profile's request settings. Zero values keep the model's or
// the SDK's defaults.
type Options struct {
	// Thinking is "" (the model's default), "adaptive" or "disabled".
	Thinking string `json:"thinking"`
	// TimeoutSeconds bounds one request attempt (10–900); 0 is the SDK default.
	TimeoutSeconds int `json:"timeout_seconds"`
	// MaxRetries for retryable failures (0–10); nil is the SDK default.
	MaxRetries *int `json:"max_retries"`
}

func (o Options) Validate() error {
	switch {
	case o.Thinking != "" && o.Thinking != "adaptive" && o.Thinking != "disabled":
		return errors.New("thinking must be adaptive, disabled or empty (model default)")
	case o.TimeoutSeconds != 0 && (o.TimeoutSeconds < 10 || o.TimeoutSeconds > 900):
		return errors.New("timeout_seconds must be 10–900, or 0 for the default")
	case o.MaxRetries != nil && (*o.MaxRetries < 0 || *o.MaxRetries > 10):
		return errors.New("max_retries must be 0–10")
	}
	return nil
}

type Request struct {
	AgentKey  string
	System    string
	Input     string         // the prompt envelope: excerpts, evidence IDs, task
	Schema    map[string]any // JSON Schema the response must satisfy
	MaxTokens int64
}

type Response struct {
	JSON         json.RawMessage
	InputTokens  int64
	OutputTokens int64
}

type Client interface {
	CompleteJSON(ctx context.Context, req Request) (Response, error)
}

// RefusalError reports that the model declined the request.
type RefusalError struct{ Category string }

func (e *RefusalError) Error() string { return "model refused the request (" + e.Category + ")" }

func NewClient(p Profile) (Client, error) {
	switch p.Provider {
	case "anthropic":
		return &anthropicClient{client: anthropic.NewClient(requestOptions(p)...), model: p.Model, effort: p.Effort, thinking: p.Options.Thinking}, nil
	}
	return nil, fmt.Errorf("llm: unsupported provider %q", p.Provider)
}

// requestOptions builds the SDK options for a profile. The stored profile is
// the only source of endpoint and credential; ANTHROPIC_* variables in the
// server environment must not redirect a profile's key elsewhere.
func requestOptions(p Profile) []option.RequestOption {
	opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithAPIKey(p.APIKey)}
	if p.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(p.BaseURL))
	}
	if p.Options.TimeoutSeconds > 0 {
		opts = append(opts, option.WithRequestTimeout(time.Duration(p.Options.TimeoutSeconds)*time.Second))
	}
	if p.Options.MaxRetries != nil {
		opts = append(opts, option.WithMaxRetries(*p.Options.MaxRetries))
	}
	return opts
}

// Check verifies a profile's key, endpoint and model by looking the model
// up; it spends no tokens. It returns the model's display name.
func Check(ctx context.Context, p Profile) (string, error) {
	if p.Provider != "anthropic" {
		return "", fmt.Errorf("llm: unsupported provider %q", p.Provider)
	}
	c := anthropic.NewClient(requestOptions(p)...)
	m, err := c.Models.Get(ctx, p.Model, anthropic.ModelGetParams{})
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case 401:
				return "", errors.New("the API key was rejected (401)")
			case 403:
				return "", errors.New("the API key is not allowed to use this model (403)")
			case 404:
				return "", fmt.Errorf("model %q was not found (404)", p.Model)
			}
			return "", fmt.Errorf("the provider answered %d", apiErr.StatusCode)
		}
		return "", fmt.Errorf("could not reach the provider: %w", err)
	}
	if m.DisplayName != "" {
		return m.DisplayName, nil
	}
	return m.ID, nil
}

type anthropicClient struct {
	client   anthropic.Client
	model    string
	effort   string
	thinking string
}

func (c *anthropicClient) CompleteJSON(ctx context.Context, req Request) (Response, error) {
	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: req.MaxTokens,
		System:    []anthropic.TextBlockParam{{Text: req.System}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.Input))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(c.effort),
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	}
	switch c.thinking {
	case "adaptive":
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
	case "disabled":
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
	}
	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return Response{}, err
	}
	out := Response{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
	switch resp.StopReason {
	case anthropic.StopReasonRefusal:
		return out, &RefusalError{Category: string(resp.StopDetails.Category)}
	case anthropic.StopReasonMaxTokens:
		return out, errors.New("llm: response truncated at max_tokens")
	}
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			if !json.Valid([]byte(text.Text)) {
				return out, errors.New("llm: response is not valid JSON")
			}
			out.JSON = json.RawMessage(text.Text)
			return out, nil
		}
	}
	return out, errors.New("llm: response has no text block")
}
