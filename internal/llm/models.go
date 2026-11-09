package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// ModelChoice is one model a profile's key can use, for picking a model
// instead of typing its ID.
type ModelChoice struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	// MaxOutput is the model's output-token limit when the provider reports it.
	MaxOutput  int64 `json:"max_output,omitempty"`
	Deprecated bool  `json:"deprecated,omitempty"`
}

// ListModels asks the provider which models the key can use. It spends no
// tokens. Errors read like Check's.
func ListModels(ctx context.Context, p Profile) ([]ModelChoice, error) {
	switch p.Provider {
	case "anthropic":
		return anthropicModels(ctx, p)
	case "openai":
		return openaiModels(ctx, p)
	}
	return nil, fmt.Errorf("llm: unsupported provider %q", p.Provider)
}

func anthropicModels(ctx context.Context, p Profile) ([]ModelChoice, error) {
	c := anthropic.NewClient(requestOptions(p)...)
	var out []ModelChoice
	it := c.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(100)})
	for it.Next() {
		m := it.Current()
		out = append(out, ModelChoice{ID: m.ID, DisplayName: m.DisplayName, MaxOutput: m.MaxTokens, Deprecated: string(m.Lifecycle) == "deprecated"})
		if len(out) >= 500 {
			break
		}
	}
	if err := it.Err(); err != nil {
		return nil, providerError(err)
	}
	// The API lists newest first; keep that, with deprecated models last.
	sort.SliceStable(out, func(i, j int) bool { return !out[i].Deprecated && out[j].Deprecated })
	if out == nil {
		out = []ModelChoice{}
	}
	return out, nil
}

// providerError turns an SDK error into the short reasons Check gives.
func providerError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401:
			return errors.New("the API key was rejected (401)")
		case 403:
			return errors.New("the API key is not allowed to list models (403)")
		}
		return fmt.Errorf("the provider answered %d", apiErr.StatusCode)
	}
	return fmt.Errorf("could not reach the provider: %w", err)
}

// openAIChatPrefixes are the model families that answer chat completions;
// the list endpoint also returns embedding, audio and image models.
var openAIChatPrefixes = []string{"gpt-", "o1", "o3", "o4", "chatgpt-"}

func openaiModels(ctx context.Context, p Profile) ([]ModelChoice, error) {
	c := newOpenAIClient(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the provider: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, errors.New("the API key was rejected (401)")
	case http.StatusForbidden:
		return nil, errors.New("the API key is not allowed to list models (403)")
	default:
		return nil, fmt.Errorf("the provider answered %d: %s", resp.StatusCode, openAIErrorMessage(data))
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("cannot parse the model list: %w", err)
	}
	var all, chat []ModelChoice
	for _, m := range body.Data {
		c := ModelChoice{ID: m.ID, DisplayName: m.ID}
		all = append(all, c)
		for _, pre := range openAIChatPrefixes {
			if strings.HasPrefix(m.ID, pre) && !strings.Contains(m.ID, "audio") && !strings.Contains(m.ID, "realtime") && !strings.Contains(m.ID, "transcribe") && !strings.Contains(m.ID, "tts") && !strings.Contains(m.ID, "image") {
				chat = append(chat, c)
				break
			}
		}
	}
	// A compatible server with its own model names: offer everything.
	if len(chat) == 0 {
		chat = all
	}
	return sortChoices(chat), nil
}

// sortChoices orders an unordered list by ID descending, so newer versions
// (which sort later lexically) come first.
func sortChoices(cs []ModelChoice) []ModelChoice {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].ID > cs[j].ID })
	if cs == nil {
		cs = []ModelChoice{}
	}
	return cs
}
