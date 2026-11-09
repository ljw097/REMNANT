package store

import (
	"context"
	"encoding/json"
	"time"
)

// Live activity: what is happening right now. A model call is recorded when
// it starts (outcome "running") and completed when it returns, so calls in
// flight are visible; runs and intakes that are still moving are listed with
// them.

// FinishModelCall completes a call recorded at its start.
func (s *Store) FinishModelCall(ctx context.Context, id int64, in ModelCallInput) error {
	if len(in.Error) > 4000 {
		in.Error = in.Error[:4000]
	}
	var resp any
	if len(in.Response) > 0 && json.Valid(in.Response) {
		resp = in.Response
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE model_calls SET input_tokens = $2, output_tokens = $3, duration_ms = $4, outcome = $5, error = $6, response = $7
		WHERE id = $1`, id, in.InputTokens, in.OutputTokens, in.Duration.Milliseconds(), in.Outcome, in.Error, resp)
	return err
}

// ExpireRunningCalls closes calls still marked running after `age`: the
// process that made them stopped before they returned.
func (s *Store) ExpireRunningCalls(ctx context.Context, age time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE model_calls SET outcome = 'stopped', error = 'the server stopped before this call returned',
		       duration_ms = (extract(epoch FROM now() - created_at) * 1000)::bigint
		WHERE outcome = 'running' AND created_at < now() - $1 * interval '1 second'`, age.Seconds())
	return tag.RowsAffected(), err
}

// ActiveRun is a run that has not finished.
type ActiveRun struct {
	ID           int64     `db:"id" json:"id"`
	Status       string    `db:"status" json:"status"`
	Project      string    `db:"project" json:"project"`
	Auto         bool      `db:"auto" json:"auto"`
	AutoDiscover bool      `db:"auto_discover" json:"auto_discover"`
	OutputTokens int64     `db:"output_tokens" json:"output_tokens"`
	InputTokens  int64     `db:"input_tokens" json:"input_tokens"`
	MaxTokens    int64     `db:"max_tokens" json:"max_tokens"`
	Working      bool      `db:"working" json:"working"` // a worker holds it right now
	Hypotheses   int64     `db:"hypotheses" json:"hypotheses"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// ActiveIntake is a repository analysis still fetching or mining.
type ActiveIntake struct {
	ID           int64     `db:"id" json:"id"`
	Status       string    `db:"status" json:"status"`
	SourceURI    string    `db:"source_uri" json:"source_uri"`
	CommitsFound int64     `db:"commits_found" json:"commits_found"`
	CommitsRead  int64     `db:"commits_read" json:"commits_read"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

type Activity struct {
	Runs    []ActiveRun    `json:"runs"`
	Intakes []ActiveIntake `json:"intakes"`
	// Calls are the model calls in flight, oldest first.
	Calls []ModelCall `json:"calls"`
	Now   time.Time   `json:"now"`
}

func (s *Store) Activity(ctx context.Context) (Activity, error) {
	a := Activity{Now: time.Now()}
	var err error
	if a.Runs, err = collect[ActiveRun](s.pool.Query(ctx, `
		SELECT r.id, r.status, p.name AS project, r.auto, r.auto_discover, r.output_tokens, r.input_tokens,
		       COALESCE((r.limits->>'max_tokens')::bigint, 0) AS max_tokens,
		       (r.lease_owner <> '' AND r.lease_expires_at > now()) AS working,
		       (SELECT count(*) FROM hypotheses h WHERE h.run_id = r.id) AS hypotheses, r.queued_at AS created_at
		FROM runs r JOIN projects p ON p.id = r.project_id
		WHERE r.status NOT IN ('completed','cancelled','failed','timed_out')
		ORDER BY (r.lease_owner <> '' AND r.lease_expires_at > now()) DESC, r.id DESC LIMIT 50`)); err != nil {
		return a, err
	}
	if a.Intakes, err = collect[ActiveIntake](s.pool.Query(ctx, `
		SELECT id, status, source_uri, commits_found, commits_read, created_at
		FROM intakes WHERE status IN ('fetching','mining') ORDER BY id DESC LIMIT 50`)); err != nil {
		return a, err
	}
	if a.Calls, err = collect[ModelCall](s.pool.Query(ctx, `SELECT `+modelCallListCols+`
		FROM model_calls WHERE outcome = 'running' ORDER BY id LIMIT 100`)); err != nil {
		return a, err
	}
	if a.Runs == nil {
		a.Runs = []ActiveRun{}
	}
	if a.Intakes == nil {
		a.Intakes = []ActiveIntake{}
	}
	if a.Calls == nil {
		a.Calls = []ModelCall{}
	}
	return a, nil
}
