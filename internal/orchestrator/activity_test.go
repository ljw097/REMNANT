package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/store"
)

// watchingModel looks at the live activity while its first Researcher call
// is in flight.
type watchingModel struct {
	fakeModel
	once sync.Once
	look func()
}

func (w *watchingModel) CompleteJSON(ctx context.Context, req llm.Request) (llm.Response, error) {
	if req.AgentKey == roleResearcher {
		w.once.Do(w.look)
	}
	return w.fakeModel.CompleteJSON(ctx, req)
}

func TestActivityShowsCallsInFlight(t *testing.T) {
	e := setup(t)
	runID := e.newRunWorkers(200000, 1)
	var during store.Activity
	m := &watchingModel{}
	m.look = func() {
		var err error
		during, err = e.st.Activity(e.ctx)
		must(t, err)
	}
	e.runOnce(e.orchestrator(m, nil))

	if len(during.Calls) != 1 || during.Calls[0].Outcome != "running" || during.Calls[0].Role != roleResearcher ||
		during.Calls[0].RunID == nil || *during.Calls[0].RunID != runID {
		t.Fatalf("calls in flight %+v", during.Calls)
	}
	if len(during.Runs) != 1 || during.Runs[0].ID != runID || !during.Runs[0].Working || during.Runs[0].Status != "hypothesis_generation" {
		t.Fatalf("active runs %+v", during.Runs)
	}
	after, err := e.st.Activity(e.ctx)
	must(t, err)
	if len(after.Calls) != 0 {
		t.Fatalf("calls still running after the run: %+v", after.Calls)
	}
	if e.count(`SELECT count(*) FROM remnant.model_calls WHERE run_id = $1 AND outcome = 'ok' AND output_tokens > 0`, runID) == 0 {
		t.Fatal("finished calls lost their results")
	}

	// A call a stopped server left running is closed as stopped.
	_, err = e.db.Exec(e.ctx, `INSERT INTO remnant.model_calls(run_id, role, outcome, created_at) VALUES ($1, 'skeptic', 'running', now() - interval '2 hours')`, runID)
	must(t, err)
	n, err := e.st.ExpireRunningCalls(e.ctx, time.Hour)
	must(t, err)
	if n != 1 || e.count(`SELECT count(*) FROM remnant.model_calls WHERE outcome = 'running'`) != 0 {
		t.Fatalf("expired %d", n)
	}
}
