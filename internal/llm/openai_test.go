package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOpenAI serves POST /chat/completions and records the last request body.
func fakeOpenAI(t *testing.T, reply map[string]any) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, `{"error":{"message":"unexpected request"}}`, http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func completion(finish, content, refusal string) map[string]any {
	msg := map[string]any{"role": "assistant", "content": content}
	if refusal != "" {
		msg["refusal"] = refusal
	}
	return map[string]any{
		"id":      "chatcmpl-1",
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 30},
	}
}

func openaiClientFor(t *testing.T, url, model string) Client {
	t.Helper()
	zero := 0
	c, err := NewClient(Profile{Provider: "openai", BaseURL: url, Model: model, Effort: "high", APIKey: "sk-test",
		Options: Options{MaxRetries: &zero}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOpenAICompleteJSONSendsSchemaAndParsesAnswer(t *testing.T) {
	srv, got := fakeOpenAI(t, completion("stop", `{"ok":true}`, ""))
	schema := map[string]any{"type": "object", "additionalProperties": false,
		"properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}}
	resp, err := openaiClientFor(t, srv.URL, "gpt-5").CompleteJSON(context.Background(),
		Request{System: "sys", Input: "task", Schema: schema, MaxTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.JSON) != `{"ok":true}` || resp.InputTokens != 120 || resp.OutputTokens != 30 {
		t.Fatalf("response = %+v", resp)
	}
	req := *got
	if req["model"] != "gpt-5" || req["max_completion_tokens"] != float64(2048) {
		t.Fatalf("request body = %v", req)
	}
	rf, _ := req["response_format"].(map[string]any)
	js, _ := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["strict"] != true || js["schema"] == nil {
		t.Fatalf("response_format = %v", rf)
	}
	msgs, _ := req["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", msgs)
	}
	sys, _ := msgs[0].(map[string]any)
	usr, _ := msgs[1].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "sys" || usr["role"] != "user" || usr["content"] != "task" {
		t.Fatalf("messages = %v", msgs)
	}
	// gpt-5 is a reasoning model, so reasoning_effort is set.
	if req["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v", req["reasoning_effort"])
	}
}

func TestOpenAIReasoningEffortOnlyForReasoningModels(t *testing.T) {
	srv, got := fakeOpenAI(t, completion("stop", `{}`, ""))
	if _, err := openaiClientFor(t, srv.URL, "gpt-4o").CompleteJSON(context.Background(),
		Request{Schema: map[string]any{"type": "object"}, MaxTokens: 2048}); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)["reasoning_effort"]; ok {
		t.Error("reasoning_effort sent to a non-reasoning model")
	}
}

func TestOpenAIEffortMapping(t *testing.T) {
	for in, want := range map[string]string{"low": "low", "medium": "medium", "high": "high", "xhigh": "high", "max": "high"} {
		if got := openaiEffort(in); got != want {
			t.Errorf("openaiEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenAIFinishReasonsAndRefusal(t *testing.T) {
	srv, _ := fakeOpenAI(t, completion("stop", "", "I can't help with that."))
	_, err := openaiClientFor(t, srv.URL, "gpt-4o").CompleteJSON(context.Background(), Request{MaxTokens: 100})
	var re *RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("refusal: err = %v", err)
	}

	srv, _ = fakeOpenAI(t, completion("length", `{"ok":`, ""))
	if _, err := openaiClientFor(t, srv.URL, "gpt-4o").CompleteJSON(context.Background(), Request{MaxTokens: 100}); err == nil {
		t.Fatal("truncated answer accepted")
	}

	srv, _ = fakeOpenAI(t, completion("stop", `not json`, ""))
	if _, err := openaiClientFor(t, srv.URL, "gpt-4o").CompleteJSON(context.Background(), Request{MaxTokens: 100}); err == nil {
		t.Fatal("non-JSON answer accepted")
	}
}

func TestOpenAIRetriesOnTransientErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(completion("stop", `{"ok":true}`, ""))
	}))
	t.Cleanup(srv.Close)
	two := 2
	c, _ := NewClient(Profile{Provider: "openai", BaseURL: srv.URL, Model: "gpt-4o", Effort: "high", APIKey: "sk-test",
		Options: Options{MaxRetries: &two}})
	resp, err := c.CompleteJSON(context.Background(), Request{Schema: map[string]any{"type": "object"}, MaxTokens: 100})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if string(resp.JSON) != `{"ok":true}` || calls != 3 {
		t.Fatalf("calls = %d, resp = %s", calls, resp.JSON)
	}
}

func TestOpenAIDoesNotRetryClientErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"error":{"message":"bad schema"}}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	five := 5
	c, _ := NewClient(Profile{Provider: "openai", BaseURL: srv.URL, Model: "gpt-4o", Effort: "high", APIKey: "sk-test",
		Options: Options{MaxRetries: &five}})
	if _, err := c.CompleteJSON(context.Background(), Request{Schema: map[string]any{"type": "object"}, MaxTokens: 100}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("400 was retried: %d calls", calls)
	}
}

func TestOpenAICheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("Authorization") != "Bearer sk-test":
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
		case r.Method == "GET" && r.URL.Path == "/models/gpt-5":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"gpt-5","object":"model"}`))
		default:
			http.Error(w, `{"error":{"message":"no such model"}}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	p := Profile{Provider: "openai", BaseURL: srv.URL, Model: "gpt-5", APIKey: "sk-test"}
	if name, err := Check(context.Background(), p); err != nil || name != "gpt-5" {
		t.Fatalf("check = %q, %v", name, err)
	}
	bad := p
	bad.APIKey = "sk-wrong"
	if _, err := Check(context.Background(), bad); err == nil || err.Error() != "the API key was rejected (401)" {
		t.Fatalf("bad key: %v", err)
	}
	missing := p
	missing.Model = "gpt-nope"
	if _, err := Check(context.Background(), missing); err == nil || err.Error() != `model "gpt-nope" was not found (404)` {
		t.Fatalf("missing model: %v", err)
	}
}
