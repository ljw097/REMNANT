package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// Token usage, read from the LLM call log (model_calls), and optional
// per-model prices an administrator enters to turn tokens into an estimated
// cost. Prices are never built in: they change, and they differ by contract.

const modelPricesKey = "model_prices"

// ModelPrice is a model's price in US dollars per million tokens.
type ModelPrice struct {
	InputPerMTok  float64 `json:"input_per_mtok"`
	OutputPerMTok float64 `json:"output_per_mtok"`
}

func (s *Store) ModelPrices(ctx context.Context) (map[string]ModelPrice, error) {
	prices := map[string]ModelPrice{}
	if _, err := s.Setting(ctx, modelPricesKey, &prices); err != nil {
		return map[string]ModelPrice{}, err
	}
	if prices == nil {
		prices = map[string]ModelPrice{}
	}
	return prices, nil
}

func (s *Store) SetModelPrices(ctx context.Context, prices map[string]ModelPrice, by int64) (map[string]ModelPrice, error) {
	if prices == nil {
		prices = map[string]ModelPrice{}
	}
	if len(prices) > 200 {
		return nil, fmt.Errorf("%w: at most 200 model prices", ErrInvalid)
	}
	for model, p := range prices {
		switch {
		case model == "" || len(model) > 200:
			return nil, fmt.Errorf("%w: model names must be 1–200 characters", ErrInvalid)
		case !validPrice(p.InputPerMTok) || !validPrice(p.OutputPerMTok):
			return nil, fmt.Errorf("%w: prices for %q must be 0–10000 dollars per million tokens", ErrInvalid, model)
		}
	}
	return prices, s.SetSetting(ctx, modelPricesKey, prices, by)
}

func validPrice(v float64) bool { return !math.IsNaN(v) && v >= 0 && v <= 10000 }

// UsageTotals sums a set of model calls. Cost is nil when a model in the set
// has no price.
type UsageTotals struct {
	Calls        int64    `json:"calls"`
	InputTokens  int64    `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	DurationMS   int64    `json:"duration_ms"`
	NotAnswered  int64    `json:"not_answered"`
	Cost         *float64 `json:"cost"`
}

// UsageRow is one group: a day, a model, an agent role, or a run/analysis.
type UsageRow struct {
	Key      string `json:"key"`
	Label    string `json:"label,omitempty"`
	RunID    *int64 `json:"run_id,omitempty"`
	IntakeID *int64 `json:"intake_id,omitempty"`
	UsageTotals
}

type Usage struct {
	Since    *time.Time  `json:"since"`
	TimeZone string      `json:"time_zone"`
	Totals   UsageTotals `json:"totals"`
	Today    UsageTotals `json:"today"`
	ByDay    []UsageRow  `json:"by_day"`
	ByModel  []UsageRow  `json:"by_model"`
	ByRole   []UsageRow  `json:"by_role"`
	// BySource are the runs and analyses that used the most output tokens.
	BySource []UsageRow `json:"by_source"`
	// Unpriced lists the models used in the period that have no price.
	Unpriced []string `json:"unpriced"`
}

// usagePart is one group's sums for one model, so it can be priced.
type usagePart struct {
	key, label      string
	runID, intakeID *int64
	model           string
	t               UsageTotals
}

// usageParts sums model calls matching cond (with args from $1) grouped by
// key and model.
func (s *Store) usageParts(ctx context.Context, key, label, from, cond string, args ...any) ([]usagePart, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+key+`, `+label+`, min(mc.run_id), min(mc.intake_id), mc.model,
		       count(*), COALESCE(sum(mc.input_tokens), 0), COALESCE(sum(mc.output_tokens), 0),
		       COALESCE(sum(mc.duration_ms), 0), count(*) FILTER (WHERE mc.outcome <> 'ok')
		FROM model_calls mc `+from+`
		WHERE `+cond+`
		GROUP BY 1, 2, mc.model`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []usagePart
	for rows.Next() {
		var p usagePart
		if err := rows.Scan(&p.key, &p.label, &p.runID, &p.intakeID, &p.model,
			&p.t.Calls, &p.t.InputTokens, &p.t.OutputTokens, &p.t.DurationMS, &p.t.NotAnswered); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Usage summarises model calls since a time (nil: all time), with days
// counted in the given IANA time zone (validated by the caller).
func (s *Store) Usage(ctx context.Context, since *time.Time, tz string) (Usage, error) {
	u := Usage{Since: since, TimeZone: tz}
	prices, err := s.ModelPrices(ctx)
	if err != nil {
		return u, err
	}
	unpriced := map[string]bool{}
	priced := func(p usagePart) UsageTotals {
		t := p.t
		if pr, ok := prices[p.model]; ok {
			c := float64(t.InputTokens)/1e6*pr.InputPerMTok + float64(t.OutputTokens)/1e6*pr.OutputPerMTok
			t.Cost = &c
		} else {
			unpriced[p.model] = true
		}
		return t
	}
	// fold merges a group's per-model parts; an unpriced part makes the
	// group's cost unknown.
	fold := func(parts []usagePart) []UsageRow {
		var order []string
		rows := map[string]*UsageRow{}
		for _, p := range parts {
			t := priced(p)
			r, ok := rows[p.key]
			if !ok {
				zero := 0.0
				r = &UsageRow{Key: p.key, Label: p.label, RunID: p.runID, IntakeID: p.intakeID, UsageTotals: UsageTotals{Cost: &zero}}
				rows[p.key] = r
				order = append(order, p.key)
			}
			r.Calls += t.Calls
			r.InputTokens += t.InputTokens
			r.OutputTokens += t.OutputTokens
			r.DurationMS += t.DurationMS
			r.NotAnswered += t.NotAnswered
			if r.Cost != nil && t.Cost != nil {
				v := *r.Cost + *t.Cost
				r.Cost = &v
			} else {
				r.Cost = nil
			}
		}
		out := make([]UsageRow, 0, len(order))
		for _, k := range order {
			out = append(out, *rows[k])
		}
		return out
	}
	total := func(rows []UsageRow) UsageTotals {
		if len(rows) == 0 {
			zero := 0.0
			return UsageTotals{Cost: &zero}
		}
		return rows[0].UsageTotals
	}

	var sinceArg any
	if since != nil {
		sinceArg = *since
	}
	inPeriod := `($1::timestamptz IS NULL OR mc.created_at >= $1)`
	query := func(key, label, from string) ([]UsageRow, error) {
		parts, err := s.usageParts(ctx, key, label, from, inPeriod, sinceArg, tz)
		if err != nil {
			return nil, err
		}
		return fold(parts), nil
	}
	// $2 (the time zone) is passed to every query; only some use it.
	const noLabel = `''::text`
	all, err := query(`'all'::text || left($2, 0)`, noLabel, ``)
	if err != nil {
		return u, err
	}
	u.Totals = total(all)
	todayParts, err := s.usageParts(ctx, `'today'::text`, noLabel, ``,
		`(mc.created_at AT TIME ZONE $1)::date = (now() AT TIME ZONE $1)::date`, tz)
	if err != nil {
		return u, err
	}
	u.Today = total(fold(todayParts))
	if u.ByDay, err = query(`to_char(mc.created_at AT TIME ZONE $2, 'YYYY-MM-DD')`, noLabel, ``); err != nil {
		return u, err
	}
	sort.Slice(u.ByDay, func(i, j int) bool { return u.ByDay[i].Key < u.ByDay[j].Key })
	if u.ByModel, err = query(`mc.model || left($2, 0)`, noLabel, ``); err != nil {
		return u, err
	}
	if u.ByRole, err = query(`mc.role || left($2, 0)`, noLabel, ``); err != nil {
		return u, err
	}
	if u.BySource, err = query(
		`(CASE WHEN mc.run_id IS NOT NULL THEN 'run-' || mc.run_id ELSE 'intake-' || mc.intake_id END) || left($2, 0)`,
		`COALESCE(p.name, i.source_uri, '')`,
		`LEFT JOIN runs r ON r.id = mc.run_id LEFT JOIN projects p ON p.id = r.project_id LEFT JOIN intakes i ON i.id = mc.intake_id`); err != nil {
		return u, err
	}
	byOutput := func(rows []UsageRow) {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].OutputTokens > rows[j].OutputTokens })
	}
	byOutput(u.ByModel)
	byOutput(u.ByRole)
	byOutput(u.BySource)
	if len(u.BySource) > 15 {
		u.BySource = u.BySource[:15]
	}
	u.Unpriced = []string{}
	for m := range unpriced {
		u.Unpriced = append(u.Unpriced, m)
	}
	sort.Strings(u.Unpriced)
	return u, nil
}
