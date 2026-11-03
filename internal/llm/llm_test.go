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

// fakeAPI serves POST /v1/messages and records the last request body.
func fakeAPI(t *testing.T, reply map[string]any) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
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

func message(stop string, text string) map[string]any {
	return map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": stop, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 120, "output_tokens": 30},
	}
}

func client(t *testing.T, url string) Client {
	t.Helper()
	c, err := NewClient(Profile{Provider: "anthropic", BaseURL: url, Model: "claude-opus-5-5", Effort: "high", APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCompleteJSONSendsSchemaAndParsesAnswer(t *testing.T) {
	srv, got := fakeAPI(t, message("end_turn", `{"ok":true}`))
	schema := map[string]any{"type": "object", "additionalProperties": false,
		"properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}}
	resp, err := client(t, srv.URL).CompleteJSON(context.Background(),
		Request{System: "sys", Input: "task", Schema: schema, MaxTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.JSON) != `{"ok":true}` || resp.InputTokens != 120 || resp.OutputTokens != 30 {
		t.Fatalf("response = %+v", resp)
	}
	req := *got
	oc, _ := req["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if req["model"] != "claude-opus-5-5" || req["max_tokens"] != float64(2048) || oc["effort"] != "high" ||
		format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("request body = %v", req)
	}
	if _, ok := req["thinking"]; ok {
		t.Error("thinking must be left to the model default")
	}
}

func TestCompleteJSONStopReasons(t *testing.T) {
	refusal := message("refusal", "")
	refusal["stop_details"] = map[string]any{"type": "refusal", "category": "cyber", "explanation": "x"}
	srv, _ := fakeAPI(t, refusal)
	_, err := client(t, srv.URL).CompleteJSON(context.Background(), Request{MaxTokens: 100})
	var re *RefusalError
	if !errors.As(err, &re) || re.Category != "cyber" {
		t.Fatalf("refusal: err = %v", err)
	}

	srv, _ = fakeAPI(t, message("max_tokens", `{"ok":`))
	if _, err := client(t, srv.URL).CompleteJSON(context.Background(), Request{MaxTokens: 100}); err == nil {
		t.Fatal("truncated answer accepted")
	}

	srv, _ = fakeAPI(t, message("end_turn", `not json`))
	if _, err := client(t, srv.URL).CompleteJSON(context.Background(), Request{MaxTokens: 100}); err == nil {
		t.Fatal("non-JSON answer accepted")
	}
}

func TestEnvironmentCannotRedirectProfileKey(t *testing.T) {
	srv, _ := fakeAPI(t, message("end_turn", `{}`))
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1") // would fail if honoured
	t.Setenv("ANTHROPIC_API_KEY", "sk-env")
	if _, err := client(t, srv.URL).CompleteJSON(context.Background(), Request{MaxTokens: 100}); err != nil {
		t.Fatalf("profile settings were overridden by the environment: %v", err)
	}
}

func TestUnsupportedProvider(t *testing.T) {
	if _, err := NewClient(Profile{Provider: "other"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestThinkingOption(t *testing.T) {
	for mode, want := range map[string]string{"adaptive": "adaptive", "disabled": "disabled"} {
		srv, got := fakeAPI(t, message("end_turn", `{}`))
		c, err := NewClient(Profile{Provider: "anthropic", BaseURL: srv.URL, Model: "claude-opus-5-5", Effort: "high", APIKey: "sk-test",
			Options: Options{Thinking: mode}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.CompleteJSON(context.Background(), Request{Schema: map[string]any{"type": "object"}, MaxTokens: 2048}); err != nil {
			t.Fatal(err)
		}
		th, _ := (*got)["thinking"].(map[string]any)
		if th["type"] != want {
			t.Errorf("%s: thinking = %v", mode, (*got)["thinking"])
		}
	}
}

func TestRetriesOption(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, 529)
	}))
	t.Cleanup(srv.Close)
	zero := 0
	c, _ := NewClient(Profile{Provider: "anthropic", BaseURL: srv.URL, Model: "m", Effort: "high", APIKey: "sk-test", Options: Options{MaxRetries: &zero}})
	if _, err := c.CompleteJSON(context.Background(), Request{Schema: map[string]any{"type": "object"}, MaxTokens: 2048}); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("max_retries 0 made %d calls", calls)
	}
}

func TestOptionsValidate(t *testing.T) {
	neg, eleven, three := -1, 11, 3
	for _, o := range []Options{{Thinking: "always"}, {TimeoutSeconds: 5}, {TimeoutSeconds: 901}, {MaxRetries: &neg}, {MaxRetries: &eleven}} {
		if o.Validate() == nil {
			t.Errorf("%+v accepted", o)
		}
	}
	for _, o := range []Options{{}, {Thinking: "adaptive", TimeoutSeconds: 120, MaxRetries: &three}} {
		if err := o.Validate(); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}
}

func TestCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("X-Api-Key") != "sk-test" && r.Header.Get("Authorization") != "Bearer sk-test":
			http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, 401)
		case r.Method == "GET" && r.URL.Path == "/v1/models/claude-opus-5-5":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-01-01T00:00:00Z"}`))
		default:
			http.Error(w, `{"type":"error","error":{"type":"not_found_error","message":"no"}}`, 404)
		}
	}))
	t.Cleanup(srv.Close)
	zero := 0
	p := Profile{Provider: "anthropic", BaseURL: srv.URL, Model: "claude-opus-5-5", APIKey: "sk-test", Options: Options{MaxRetries: &zero}}
	if name, err := Check(context.Background(), p); err != nil || name != "Claude Opus 5.5" {
		t.Fatalf("check = %q, %v", name, err)
	}
	bad := p
	bad.APIKey = "sk-wrong"
	if _, err := Check(context.Background(), bad); err == nil || err.Error() != "the API key was rejected (401)" {
		t.Fatalf("bad key: %v", err)
	}
	missing := p
	missing.Model = "claude-nope"
	if _, err := Check(context.Background(), missing); err == nil || err.Error() != `model "claude-nope" was not found (404)` {
		t.Fatalf("missing model: %v", err)
	}
}
