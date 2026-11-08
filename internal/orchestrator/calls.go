package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/store"
)

// errNoProfile: neither the role's override nor the caller names a model profile.
var errNoProfile = errors.New("no model profile")

// newAgent builds the agent for a role from its agent_profiles row: whether
// it runs at all, which model profile it uses (the role's override, else
// fallbackProfile), its extra instructions and its per-call token cap (the
// role's, else the research settings', else the default). It returns nil, nil
// when the role is turned off. A non-nil cache shares one client per model
// profile between roles.
func (o *Orchestrator) newAgent(ctx context.Context, role string, fallbackProfile *int64, settings store.ResearchSettings, cache map[int64]llm.Client) (*agent, error) {
	enabled, override, cfg, err := o.store.AgentProfile(ctx, role)
	if err != nil || !enabled {
		return nil, err
	}
	profileID := fallbackProfile
	if override != nil {
		profileID = override
	}
	if profileID == nil {
		return nil, fmt.Errorf("%s: %w", role, errNoProfile)
	}
	creds, err := o.store.LLMCredentials(ctx, *profileID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", role, err)
	}
	client, ok := cache[*profileID]
	if !ok {
		if client, err = o.opts.NewClient(creds); err != nil {
			return nil, fmt.Errorf("%s: %w", role, err)
		}
		if cache != nil {
			cache[*profileID] = client
		}
	}
	callTokens := cfg.MaxCallTokens
	if callTokens <= 0 {
		callTokens = settings.MaxCallTokens
	}
	if callTokens <= 0 {
		callTokens = store.DefaultResearchSettings().MaxCallTokens
	}
	return &agent{role: role, client: client, instructions: cfg.Instructions, maxCallTokens: int64(callTokens), model: creds.Model}, nil
}

// callRef says what a model call is about, for the call log.
type callRef struct {
	RunID, IntakeID          *int64
	Subject                  string
	EvidenceID, HypothesisID *int64
	CommitSHA                string
}

// complete makes one model call and records it in model_calls: the prompt
// envelope, the answer, tokens, duration and how the call ended. The call
// ID is returned so the caller can record what it made of the answer.
// Recording never fails the call.
func (o *Orchestrator) complete(ctx context.Context, a *agent, ref callRef, req llm.Request) (llm.Response, int64, error) {
	start := time.Now()
	resp, err := a.client.CompleteJSON(ctx, req)
	in := store.ModelCallInput{
		RunID: ref.RunID, IntakeID: ref.IntakeID, Role: a.role, Subject: ref.Subject,
		EvidenceID: ref.EvidenceID, HypothesisID: ref.HypothesisID, CommitSHA: ref.CommitSHA, Model: a.model,
		MaxTokens: req.MaxTokens, InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens,
		Duration: time.Since(start), Outcome: callOutcome(ctx, err), Request: req.Input, Response: resp.JSON,
	}
	if err != nil {
		in.Error = err.Error()
	}
	bg, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	id, rerr := o.store.InsertModelCall(bg, in)
	if rerr != nil {
		o.log.Warn("record model call", "role", a.role, "err", rerr)
	}
	return resp, id, err
}

func callOutcome(ctx context.Context, err error) string {
	var refusal *llm.RefusalError
	switch {
	case err == nil:
		return "ok"
	case ctx.Err() != nil:
		return "stopped"
	case errors.As(err, &refusal):
		return "refused"
	case strings.Contains(err.Error(), "truncated at max"):
		return "truncated"
	}
	return "error"
}

// callResult records what REMNANT made of an answer. Best effort.
func (o *Orchestrator) callResult(ctx context.Context, id int64, result string, hypothesisID *int64) {
	if id == 0 {
		return
	}
	bg, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	if err := o.store.SetModelCallResult(bg, id, result, hypothesisID); err != nil {
		o.log.Warn("record model call result", "call", id, "err", err)
	}
}

// failedResult describes, for the call log, what happened after a call that
// did not return a usable answer (kind from callFailed).
func failedResult(kind string, err error) string {
	if err != nil && strings.Contains(err.Error(), "truncated at max") {
		return "answer cut off at the per-call token cap; raise tokens per model call"
	}
	switch kind {
	case "refused":
		return "refused by the model; region skipped"
	case "stopped":
		return "run stopped; retried when it resumes"
	case "budget":
		return "token budget exhausted"
	}
	return "call failed; region skipped"
}
