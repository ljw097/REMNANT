// Package llm turns a stored provider profile into a client that returns
// schema-constrained JSON. Agents only ever see this interface.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
		// The stored profile is the only source of endpoint and credential;
		// ANTHROPIC_* variables in the server environment must not redirect
		// a profile's key elsewhere.
		opts := []option.RequestOption{option.WithoutEnvironmentDefaults(), option.WithAPIKey(p.APIKey)}
		if p.BaseURL != "" {
			opts = append(opts, option.WithBaseURL(p.BaseURL))
		}
		return &anthropicClient{client: anthropic.NewClient(opts...), model: p.Model, effort: p.Effort}, nil
	}
	return nil, fmt.Errorf("llm: unsupported provider %q", p.Provider)
}

type anthropicClient struct {
	client anthropic.Client
	model  string
	effort string
}

func (c *anthropicClient) CompleteJSON(ctx context.Context, req Request) (Response, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: req.MaxTokens,
		System:    []anthropic.TextBlockParam{{Text: req.System}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.Input))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(c.effort),
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	})
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
