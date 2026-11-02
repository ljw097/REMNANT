package worker

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func exampleRequest(t *testing.T) Request {
	t.Helper()
	b, err := os.ReadFile("../../examples/worker_request.json")
	if err != nil {
		t.Fatal(err)
	}
	var req Request
	if err := json.Unmarshal(b, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestExamplesValidate(t *testing.T) {
	req := exampleRequest(t)
	if err := ValidateRequest(req); err != nil {
		t.Fatalf("example request: %v", err)
	}
	b, err := os.ReadFile("../../examples/worker_response.json")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := ParseLine(b)
	if err != nil {
		t.Fatal(err)
	}
	res, ok := msg.(*Result)
	if !ok {
		t.Fatalf("ParseLine returned %T", msg)
	}
	if err := ValidateResult(req, *res); err != nil {
		t.Fatalf("example response: %v", err)
	}
}

func TestValidateRequestRejects(t *testing.T) {
	mutations := map[string]func(*Request){
		"unknown op":       func(r *Request) { r.Operation = "shell.exec" },
		"lab w/o approval": func(r *Request) { r.Operation = "lab.unit_test" },
		"network":          func(r *Request) { r.Policy.Network = "bridge" },
		"escaping root":    func(r *Request) { r.Workspace.RelativeRoot = "../other" },
		"no revision":      func(r *Request) { r.Workspace.Revision = "" },
		"zero limit":       func(r *Request) { r.Limits.MemoryMB = 0 },
		"wrong protocol":   func(r *Request) { r.Protocol = "v0" },
	}
	for name, mutate := range mutations {
		req := exampleRequest(t)
		mutate(&req)
		if ValidateRequest(req) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	req := exampleRequest(t)
	req.Operation, req.ApprovalID = "lab.unit_test", 9
	if err := ValidateRequest(req); err != nil {
		t.Errorf("approved lab op rejected: %v", err)
	}
}

func TestValidateResultRejects(t *testing.T) {
	req := exampleRequest(t)
	good := func() Result {
		return Result{Protocol: Protocol, Message: "result", RequestID: req.RequestID, Status: "completed",
			Observations: []Observation{{Kind: "static_finding", FilePath: "src/a.c", LineStart: 3, LineEnd: 4, Message: "m"}},
			Artifacts:    []Artifact{{Name: "out.sarif", SHA256: strings.Repeat("a", 64), SizeBytes: 10}}}
	}
	if err := ValidateResult(req, good()); err != nil {
		t.Fatalf("good result rejected: %v", err)
	}
	mutations := map[string]func(*Result){
		"request id":    func(r *Result) { r.RequestID = "other" },
		"traversal":     func(r *Result) { r.Observations[0].FilePath = "../../etc/passwd" },
		"absolute":      func(r *Result) { r.Observations[0].FilePath = "/etc/passwd" },
		"unclean":       func(r *Result) { r.Observations[0].FilePath = "src/./a.c" },
		"line range":    func(r *Result) { r.Observations[0].LineEnd = 1 },
		"confidence":    func(r *Result) { r.Observations[0].ToolConfidence = "certain" },
		"bad digest":    func(r *Result) { r.Artifacts[0].SHA256 = "XYZ" },
		"artifact name": func(r *Result) { r.Artifacts[0].Name = "../x" },
		"too large":     func(r *Result) { r.Artifacts[0].SizeBytes = req.Limits.MaxOutputBytes + 1 },
		"status":        func(r *Result) { r.Status = "pwned" },
	}
	for name, mutate := range mutations {
		r := good()
		mutate(&r)
		if ValidateResult(req, r) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseLine(t *testing.T) {
	if _, err := ParseLine([]byte(`{"protocol":"other","message":"result"}`)); err == nil {
		t.Error("foreign protocol accepted")
	}
	if _, err := ParseLine([]byte(`{"protocol":"remnant.worker.v1","message":"request"}`)); err == nil {
		t.Error("request message accepted from worker")
	}
	msg, err := ParseLine([]byte(`{"protocol":"remnant.worker.v1","message":"error","code":"RESOURCE_LIMIT","summary":"oom"}`))
	if e, ok := msg.(*Error); err != nil || !ok || e.Code != "RESOURCE_LIMIT" {
		t.Errorf("error line = %#v, %v", msg, err)
	}
}
