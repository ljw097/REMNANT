package research

import "testing"

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to RunStatus
		want     bool
	}{
		{RunQueued, RunCollecting, true},
		{RunQueued, RunIndexing, false},
		{RunStaticValidation, RunReviewRequired, true},
		{RunReviewRequired, RunLabValidation, false}, // approval must come first
		{RunReviewRequired, RunValidationApproved, true},
		{RunValidationApproved, RunLabValidation, true},
		{RunLabValidation, RunReportReady, true},
		{RunIndexing, RunCancelled, true},
		{RunIndexing, RunTimedOut, true},
		{RunCompleted, RunCancelled, false},
		{RunCancelled, RunQueued, false},
		{RunQueued, "bogus", false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestEveryForwardTargetIsValid(t *testing.T) {
	for from, tos := range forward {
		for _, to := range tos {
			if !to.Valid() {
				t.Errorf("%s -> %s: target not a known status", from, to)
			}
		}
	}
}

func TestLimitsNormalize(t *testing.T) {
	l, err := Limits{}.Normalize()
	if err != nil || l != (Limits{2, 900, 200000}) {
		t.Fatalf("defaults = %+v, %v", l, err)
	}
	for _, bad := range []Limits{{MaxWorkers: 5}, {WallSeconds: 30}, {MaxTokens: 3000}, {MaxTokens: 3000000}, {MaxWorkers: -1}} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestCanTriage(t *testing.T) {
	cases := []struct {
		s    HypothesisStatus
		d    TriageDecision
		want bool
	}{
		{HypothesisDraft, DecisionAccepted, true},
		{HypothesisTriage, DecisionRejected, true},
		{HypothesisAccepted, DecisionRejected, false},
		{HypothesisValidated, DecisionAccepted, false},
		{HypothesisTriage, DecisionValidated, false}, // only after a lab run
		{HypothesisValidationPending, DecisionValidated, true},
		{HypothesisValidationPending, DecisionNotReproducible, true},
		{HypothesisValidationPending, DecisionAccepted, false},
	}
	for _, c := range cases {
		if got := CanTriage(c.s, c.d); got != c.want {
			t.Errorf("CanTriage(%s, %s) = %v, want %v", c.s, c.d, got, c.want)
		}
	}
}
