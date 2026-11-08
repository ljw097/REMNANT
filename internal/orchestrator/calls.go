package orchestrator

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/store"
)

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

func ptr[T any](v T) *T { return &v }
