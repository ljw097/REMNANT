package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModels(t *testing.T) {
	ant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Api-Key") != "sk-ant-good" && r.Header.Get("Authorization") != "Bearer sk-ant-good" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"type":"model","id":"claude-old","display_name":"Old","created_at":"2025-01-01T00:00:00Z","max_tokens":8192,"lifecycle":"deprecated"},
			{"type":"model","id":"claude-new","display_name":"New","created_at":"2026-01-01T00:00:00Z","max_tokens":64000,"lifecycle":"active"}],
			"has_more":false,"first_id":"claude-old","last_id":"claude-new"}`))
	}))
	defer ant.Close()
	ms, err := ListModels(context.Background(), Profile{Provider: "anthropic", BaseURL: ant.URL, APIKey: "sk-ant-good"})
	if err != nil || len(ms) != 2 || ms[0].ID != "claude-new" || ms[0].MaxOutput != 64000 || !ms[1].Deprecated {
		t.Fatalf("anthropic models %+v %v", ms, err)
	}
	if _, err := ListModels(context.Background(), Profile{Provider: "anthropic", BaseURL: ant.URL, APIKey: "sk-ant-bad"}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad key: %v", err)
	}

	oai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-3-small"},{"id":"gpt-4o"},{"id":"gpt-5"},{"id":"gpt-4o-audio-preview"},{"id":"o3"}]}`))
	}))
	defer oai.Close()
	ms, err = ListModels(context.Background(), Profile{Provider: "openai", BaseURL: oai.URL, APIKey: "sk-x"})
	ids := []string{}
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if err != nil || strings.Join(ids, ",") != "o3,gpt-5,gpt-4o" {
		t.Fatalf("openai chat models %v %v", ids, err)
	}

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"llama-3-70b"},{"id":"qwen-coder"}]}`))
	}))
	defer local.Close()
	ms, err = ListModels(context.Background(), Profile{Provider: "openai", BaseURL: local.URL, APIKey: "x"})
	if err != nil || len(ms) != 2 {
		t.Fatalf("compatible server models %+v %v", ms, err)
	}
}
