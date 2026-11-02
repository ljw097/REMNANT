package policy

import "testing"

func TestValidate(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default policy invalid: %v", err)
	}
	lab := Default()
	lab.DynamicMode = DynamicLocalLab
	if err := lab.Validate(); err != nil {
		t.Fatalf("local lab policy invalid: %v", err)
	}
	for _, bad := range []Policy{
		{SourceMode: "read_write", DynamicMode: DynamicDisabled, Network: NetworkNone},
		{SourceMode: SourceReadOnly, DynamicMode: "remote", Network: NetworkNone},
		{SourceMode: SourceReadOnly, DynamicMode: DynamicDisabled, Network: "egress"},
	} {
		if bad.Validate() == nil {
			t.Errorf("policy %+v accepted", bad)
		}
	}
}

func TestRunScopeHashIsOrderIndependentAndRevisionSensitive(t *testing.T) {
	a := RunScope{
		Policy:   Default(),
		Targets:  []TargetRevision{{1, "aaa"}, {2, "bbb"}},
		Patterns: []int64{7, 3},
		Limits:   map[string]int{"max_workers": 2},
	}
	b := a
	b.Targets = []TargetRevision{{2, "bbb"}, {1, "aaa"}}
	b.Patterns = []int64{3, 7}
	ha, _ := a.Hash()
	hb, _ := b.Hash()
	if ha != hb {
		t.Fatalf("hash depends on order: %s != %s", ha, hb)
	}
	if a.Targets[0].TargetID != 1 {
		t.Fatal("Hash mutated caller's slice")
	}

	c := a
	c.Targets = []TargetRevision{{1, "aaa"}, {2, "ccc"}}
	hc, _ := c.Hash()
	if hc == ha {
		t.Fatal("revision change did not change hash")
	}
}

func TestLabScopeHashChangesWithLimits(t *testing.T) {
	s := LabScope{ActionClass: ActionLocalLab, RunScopeHash: "x", HypothesisID: 1, TargetID: 2, Revision: "r", Network: NetworkNone, Limits: DefaultLabLimits()}
	h1, _ := s.Hash()
	s.Limits.MemoryMB++
	h2, _ := s.Hash()
	if h1 == h2 {
		t.Fatal("limit change did not change hash")
	}
}
