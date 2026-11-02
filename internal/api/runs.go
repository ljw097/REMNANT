package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ljw097/remnant/internal/policy"
	"github.com/ljw097/remnant/internal/research"
	"github.com/ljw097/remnant/internal/store"
	"github.com/ljw097/remnant/internal/worker"
)

func (s *Server) searchPatterns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ps, err := s.store.SearchPatterns(r.Context(), q.Get("q"), q.Get("vulnerability_class"), q.Get("language"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createPattern(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourceKind         string         `json:"source_kind"`
		SourceRef          string         `json:"source_ref"`
		Title              string         `json:"title"`
		VulnerabilityClass string         `json:"vulnerability_class"`
		CWEID              string         `json:"cwe_id"`
		Language           string         `json:"language"`
		RootCause          string         `json:"root_cause"`
		Preconditions      []string       `json:"preconditions"`
		LifecyclePoints    []string       `json:"lifecycle_points"`
		CodeSignatures     []string       `json:"code_signatures"`
		CounterSignatures  []string       `json:"counter_signatures"`
		PatchSemantics     map[string]any `json:"patch_semantics"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.SourceKind == "" {
		in.SourceKind = "researcher_authored"
	}
	// validated_internal patterns are created only by promotion, never directly.
	if !validate(w,
		oneOf("source_kind", in.SourceKind, "historical", "researcher_authored"),
		required("title", in.Title, 300),
		required("vulnerability_class", in.VulnerabilityClass, 100),
		required("root_cause", in.RootCause, 20000),
		maxLen("source_ref", in.SourceRef, 2048),
		maxLen("cwe_id", in.CWEID, 20),
		maxLen("language", in.Language, 40),
	) {
		return
	}
	p, err := s.store.CreatePattern(r.Context(), store.PatternInput{
		SourceKind: in.SourceKind, SourceRef: in.SourceRef, Title: in.Title,
		VulnerabilityClass: in.VulnerabilityClass, CWEID: in.CWEID, Language: in.Language,
		RootCause: in.RootCause, Preconditions: in.Preconditions, LifecyclePoints: in.LifecyclePoints,
		CodeSignatures: in.CodeSignatures, CounterSignatures: in.CounterSignatures, PatchSemantics: in.PatchSemantics,
	}, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) reviewPattern(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &in) || !validate(w, oneOf("status", in.Status, "reviewed", "validated", "rejected")) {
		return
	}
	p, err := s.store.ReviewPattern(r.Context(), id, in.Status, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var projectID int64
	if v := q.Get("project_id"); v != "" {
		var err error
		if projectID, err = strconv.ParseInt(v, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid project_id")
			return
		}
	}
	runs, err := s.store.ListRuns(r.Context(), projectID, q.Get("status"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID    int64           `json:"project_id"`
		TargetIDs    []int64         `json:"target_ids"`
		PatternIDs   []int64         `json:"pattern_ids"`
		LLMProfileID *int64          `json:"llm_profile_id"`
		Limits       research.Limits `json:"limits"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.TargetIDs, in.PatternIDs = dedupe(in.TargetIDs), dedupe(in.PatternIDs)
	limits, limitErr := in.Limits.Normalize()
	var countErr error
	switch {
	case in.ProjectID <= 0:
		countErr = badRequest("project_id is required")
	case len(in.TargetIDs) < 1 || len(in.TargetIDs) > 10:
		countErr = badRequest("target_ids must contain 1–10 targets")
	case len(in.PatternIDs) > 100:
		countErr = badRequest("pattern_ids may contain at most 100 patterns")
	}
	if !validate(w, countErr, limitErr) {
		return
	}
	run, err := s.store.CreateRun(r.Context(), store.RunInput{
		ProjectID: in.ProjectID, TargetIDs: in.TargetIDs, PatternIDs: in.PatternIDs,
		LLMProfileID: in.LLMProfileID, Limits: limits,
	}, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	run, err := s.store.GetRun(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.CancelRun(r.Context(), id, principal(r).Username); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": string(research.RunCancelled)})
}

func (s *Server) finalizeRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.FinalizeRun(r.Context(), id, principal(r).Username); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": string(research.RunReportReady)})
}

func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	es, err := s.store.ListExperiments(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, es)
}

// artifactContent serves an artifact stored under REMNANT_DATA_DIR/artifacts.
// Worker artifacts whose bytes were never transferred return 404.
func (s *Server) artifactContent(w http.ResponseWriter, r *http.Request) {
	runID, ok := pathID(w, r)
	if !ok {
		return
	}
	artifactID, err := strconv.ParseInt(r.PathValue("artifactId"), 10, 64)
	if err != nil || artifactID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid artifact id")
		return
	}
	a, err := s.store.GetArtifact(r.Context(), runID, artifactID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	root, err := os.OpenRoot(s.cfg.ArtifactDir())
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "artifact content not stored")
		return
	}
	defer root.Close()
	f, err := root.Open(filepath.FromSlash(a.StorageKey))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "artifact content not stored")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", a.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Name}))
	w.Header().Set("X-Content-SHA256", a.SHA256)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	as, err := s.store.ListArtifacts(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, as)
}

// runEvents streams the append-only run timeline as Server-Sent Events.
// Clients resume with Last-Event-ID (or ?after=). The stream ends with an
// "end" event once the run is terminal and fully delivered.
func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "streaming unsupported")
		return
	}
	after := int64(0)
	for _, v := range []string{r.Header.Get("Last-Event-ID"), r.URL.Query().Get("after")} {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > after {
			after = n
		}
	}
	// Fail with a normal status before committing to the stream.
	if _, _, err := s.store.EventsSince(r.Context(), id, after, 1); err != nil {
		s.fail(w, r, err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	lastWrite := time.Now()
	for {
		events, status, err := s.store.EventsSince(r.Context(), id, after, 100)
		if err != nil {
			if r.Context().Err() == nil {
				s.log.Error("event stream", "run", id, "err", err)
			}
			return
		}
		for _, e := range events {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.EventType, b)
			after = e.ID
		}
		if len(events) > 0 {
			lastWrite = time.Now()
			flusher.Flush()
		}
		if len(events) < 100 && status.Terminal() {
			fmt.Fprintf(w, "event: end\ndata: {\"status\":%q}\n\n", status)
			flusher.Flush()
			return
		}
		if time.Since(lastWrite) > 15*time.Second {
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
			lastWrite = time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
		}
	}
}

func (s *Server) listHypotheses(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	hs, err := s.store.ListHypotheses(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, hs)
}

func (s *Server) getHypothesis(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := s.store.GetHypothesis(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) triageHypothesis(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Decision     research.TriageDecision `json:"decision"`
		ReviewerNote string                  `json:"reviewer_note"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Decision.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "decision must be accepted, rejected, duplicate, validated or not_reproducible")
		return
	}
	if !validate(w, maxLen("reviewer_note", in.ReviewerNote, 8000)) {
		return
	}
	h, err := s.store.TriageHypothesis(r.Context(), id, in.Decision, in.ReviewerNote, principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

const approvalTTL = 24 * time.Hour

func (s *Server) requestLabApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Operation string            `json:"operation"`
		Summary   string            `json:"summary"`
		Limits    *policy.LabLimits `json:"limits"`
	}
	if !decode(w, r, &in) {
		return
	}
	limits := policy.DefaultLabLimits()
	if in.Limits != nil {
		limits = *in.Limits
	}
	var limitErr error
	if limits.WallSeconds < 1 || limits.WallSeconds > 3600 || limits.MemoryMB < 64 || limits.MemoryMB > 16384 ||
		limits.CPUCores < 1 || limits.CPUCores > 8 || limits.MaxProcesses < 1 || limits.MaxProcesses > 256 {
		limitErr = badRequest("limits out of range (wall 1–3600s, memory 64–16384MB, cpu 1–8, processes 1–256)")
	}
	var opErr error
	if op, ok := worker.Operations[in.Operation]; !ok || !op.RequiresApproval {
		opErr = badRequest("operation must be an allowlisted lab.* operation")
	}
	if !validate(w, opErr, required("summary", in.Summary, 8000), limitErr) {
		return
	}
	a, err := s.store.RequestLabApproval(r.Context(), id, principal(r).UserID, in.Operation, limits, in.Summary, approvalTTL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Decision     string `json:"decision"`
		ReviewerNote string `json:"reviewer_note"`
	}
	if !decode(w, r, &in) ||
		!validate(w, oneOf("decision", in.Decision, "approved", "denied"), maxLen("reviewer_note", in.ReviewerNote, 8000)) {
		return
	}
	a, err := s.store.DecideApproval(r.Context(), id, principal(r).UserID, in.Decision == "approved", in.ReviewerNote)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
