package orchestrator

import (
	"errors"
	"math"
	"testing"

	"github.com/ljw097/remnant/internal/store"
)

func TestUsage(t *testing.T) {
	e := setup(t)
	runID := e.newRun(200000)
	call := func(model, role, outcome string, in, out int64) {
		_, err := e.st.InsertModelCall(e.ctx, store.ModelCallInput{RunID: &runID, Role: role, Model: model, Outcome: outcome, InputTokens: in, OutputTokens: out})
		must(t, err)
	}
	call("claude-opus-5-5", roleResearcher, "ok", 1_000_000, 200_000)
	call("claude-opus-5-5", roleSkeptic, "ok", 500_000, 100_000)
	call("claude-haiku-5-5", roleResearcher, "truncated", 100_000, 16_000)

	u, err := e.st.Usage(e.ctx, nil, "Asia/Seoul")
	must(t, err)
	if u.Totals.Calls != 3 || u.Totals.InputTokens != 1_600_000 || u.Totals.OutputTokens != 316_000 || u.Totals.NotAnswered != 1 {
		t.Fatalf("totals %+v", u.Totals)
	}
	if u.Today.Calls != 3 || len(u.ByDay) != 1 || len(u.ByModel) != 2 || u.ByModel[0].Key != "claude-opus-5-5" || len(u.ByRole) != 2 {
		t.Fatalf("groups today %+v days %+v models %+v roles %+v", u.Today, u.ByDay, u.ByModel, u.ByRole)
	}
	if len(u.BySource) != 1 || u.BySource[0].RunID == nil || *u.BySource[0].RunID != runID || u.BySource[0].Label != "demo" {
		t.Fatalf("by source %+v", u.BySource)
	}
	if u.Totals.Cost != nil || len(u.Unpriced) != 2 {
		t.Fatalf("without prices: cost %v unpriced %v", u.Totals.Cost, u.Unpriced)
	}

	// Prices for one model: its rows get a cost; totals stay unknown.
	_, err = e.st.SetModelPrices(e.ctx, map[string]store.ModelPrice{"claude-opus-5-5": {InputPerMTok: 5, OutputPerMTok: 25}}, e.alice)
	must(t, err)
	u, err = e.st.Usage(e.ctx, nil, "UTC")
	must(t, err)
	opus := u.ByModel[0]
	if opus.Cost == nil || math.Abs(*opus.Cost-(1.5*5+0.3*25)) > 1e-9 || u.Totals.Cost != nil || len(u.Unpriced) != 1 {
		t.Fatalf("one price: opus %+v totals %+v unpriced %v", opus, u.Totals, u.Unpriced)
	}
	_, err = e.st.SetModelPrices(e.ctx, map[string]store.ModelPrice{"claude-opus-5-5": {InputPerMTok: 5, OutputPerMTok: 25}, "claude-haiku-5-5": {InputPerMTok: 1, OutputPerMTok: 5}}, e.alice)
	must(t, err)
	u, err = e.st.Usage(e.ctx, nil, "UTC")
	must(t, err)
	if u.Totals.Cost == nil || math.Abs(*u.Totals.Cost-(15+0.1+0.08)) > 1e-9 || len(u.Unpriced) != 0 {
		t.Fatalf("all priced: totals %+v", u.Totals)
	}
	if _, err := e.st.SetModelPrices(e.ctx, map[string]store.ModelPrice{"x": {InputPerMTok: -1}}, e.alice); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("negative price: %v", err)
	}
}
