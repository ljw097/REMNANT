package store

import (
	"context"
	"encoding/json"
	"time"
)

// maxRecordedRequest caps the stored prompt envelope. Envelopes hold source
// excerpts (each already capped) and are normally far smaller.
const maxRecordedRequest = 256 << 10

// ModelCall is one recorded agent call. The list endpoints leave Request and
// Response out; GetModelCall returns them.
type ModelCall struct {
	ID           int64           `db:"id" json:"id"`
	RunID        *int64          `db:"run_id" json:"run_id"`
	IntakeID     *int64          `db:"intake_id" json:"intake_id"`
	Role         string          `db:"role" json:"role"`
	Subject      string          `db:"subject" json:"subject"`
	EvidenceID   *int64          `db:"evidence_id" json:"evidence_id"`
	HypothesisID *int64          `db:"hypothesis_id" json:"hypothesis_id"`
	CommitSHA    string          `db:"commit_sha" json:"commit_sha"`
	Model        string          `db:"model" json:"model"`
	MaxTokens    int64           `db:"max_tokens" json:"max_tokens"`
	InputTokens  int64           `db:"input_tokens" json:"input_tokens"`
	OutputTokens int64           `db:"output_tokens" json:"output_tokens"`
	DurationMS   int64           `db:"duration_ms" json:"duration_ms"`
	Outcome      string          `db:"outcome" json:"outcome"`
	Result       string          `db:"result" json:"result"`
	Error        string          `db:"error" json:"error"`
	Request      string          `db:"request" json:"request,omitempty"`
	Response     json.RawMessage `db:"response" json:"response,omitempty"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
}

type ModelCallInput struct {
	RunID, IntakeID           *int64
	Role, Subject             string
	EvidenceID, HypothesisID  *int64
	CommitSHA, Model          string
	MaxTokens                 int64
	InputTokens, OutputTokens int64
	Duration                  time.Duration
	Outcome, Result, Error    string
	Request                   string
	Response                  json.RawMessage
}

func (s *Store) InsertModelCall(ctx context.Context, in ModelCallInput) (int64, error) {
	if len(in.Request) > maxRecordedRequest {
		in.Request = in.Request[:maxRecordedRequest] + "\n… (truncated for storage)"
	}
	if len(in.Error) > 4000 {
		in.Error = in.Error[:4000]
	}
	var resp any
	if len(in.Response) > 0 && json.Valid(in.Response) {
		resp = in.Response
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO model_calls(run_id, intake_id, role, subject, evidence_id, hypothesis_id, commit_sha, model,
		                        max_tokens, input_tokens, output_tokens, duration_ms, outcome, result, error, request, response)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id`,
		in.RunID, in.IntakeID, in.Role, in.Subject, in.EvidenceID, in.HypothesisID, in.CommitSHA, in.Model,
		in.MaxTokens, in.InputTokens, in.OutputTokens, in.Duration.Milliseconds(), in.Outcome, in.Result, in.Error,
		in.Request, resp).Scan(&id)
	return id, mapErr(err)
}

// SetModelCallResult records what REMNANT made of an answer, and the
// hypothesis it produced or concerned, once that is known.
func (s *Store) SetModelCallResult(ctx context.Context, id int64, result string, hypothesisID *int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE model_calls SET result = $2, hypothesis_id = COALESCE($3, hypothesis_id) WHERE id = $1`, id, result, hypothesisID)
	return err
}

const modelCallListCols = `id, run_id, intake_id, role, subject, evidence_id, hypothesis_id, commit_sha, model,
	max_tokens, input_tokens, output_tokens, duration_ms, outcome, result, error, ''::text AS request, NULL::jsonb AS response, created_at`

// ModelCallFilter selects calls by owner; exactly one field is set.
type ModelCallFilter struct {
	RunID, IntakeID, HypothesisID *int64
}

func (s *Store) ListModelCalls(ctx context.Context, f ModelCallFilter) ([]ModelCall, error) {
	where, arg := "run_id = $1", any(nil)
	switch {
	case f.RunID != nil:
		arg = *f.RunID
	case f.IntakeID != nil:
		where, arg = "intake_id = $1", *f.IntakeID
	case f.HypothesisID != nil:
		// The Researcher call that drafted a hypothesis is linked once the
		// hypothesis exists; the Skeptic call carries it from the start.
		where, arg = "hypothesis_id = $1", *f.HypothesisID
	default:
		return []ModelCall{}, nil
	}
	out, err := collect[ModelCall](s.pool.Query(ctx, `SELECT `+modelCallListCols+` FROM model_calls WHERE `+where+` ORDER BY id LIMIT 2000`, arg))
	if out == nil {
		out = []ModelCall{}
	}
	return out, err
}

func (s *Store) GetModelCall(ctx context.Context, id int64) (ModelCall, error) {
	return one[ModelCall](s.pool.Query(ctx, `
		SELECT id, run_id, intake_id, role, subject, evidence_id, hypothesis_id, commit_sha, model,
		       max_tokens, input_tokens, output_tokens, duration_ms, outcome, result, error, request, response, created_at
		FROM model_calls WHERE id = $1`, id))
}

// CommitRecord returns one recorded commit of a target, with its diff.
func (s *Store) CommitRecord(ctx context.Context, targetID int64, sha string) (CommitRecord, error) {
	return one[CommitRecord](s.pool.Query(ctx, `
		SELECT id, target_id, sha, committed_at, subject, body, files, diff, signals, score
		FROM commit_records WHERE target_id = $1 AND sha = $2`, targetID, sha))
}
