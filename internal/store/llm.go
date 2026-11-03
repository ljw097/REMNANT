package store

import (
	"context"
	"fmt"

	"github.com/ljw097/remnant/internal/llm"
	"github.com/ljw097/remnant/internal/secrets"
)

// llmKeyAAD binds sealed keys to this column.
var llmKeyAAD = []byte("remnant/llm_profiles/api_key/v1")

const llmProfileCols = `id, name, provider, base_url, model, effort,
	api_key_sealed IS NOT NULL AS has_api_key, api_key_hint, options,
	(SELECT count(*) FROM projects pr WHERE pr.llm_profile_id = llm_profiles.id AND pr.status <> 'archived')::int
	  + (SELECT count(*) FROM agent_profiles a WHERE a.llm_profile_id = llm_profiles.id)::int AS used_by,
	updated_at`

type LLMProfileInput struct {
	Name     string
	Provider string
	BaseURL  string
	Model    string
	Effort   string
	APIKey   string
	Options  llm.Options
}

// LLMProfilePatch changes a profile; nil fields stay. A non-nil APIKey
// replaces the stored key.
type LLMProfilePatch struct {
	Name    *string
	BaseURL *string
	Model   *string
	Effort  *string
	APIKey  *string
	Options *llm.Options
}

func (s *Store) CreateLLMProfile(ctx context.Context, in LLMProfileInput) (LLMProfile, error) {
	var sealed []byte
	if in.APIKey != "" {
		var err error
		if sealed, err = s.sealer.Seal([]byte(in.APIKey), llmKeyAAD); err != nil {
			return LLMProfile{}, err
		}
	}
	p, err := one[LLMProfile](s.pool.Query(ctx, `
		INSERT INTO llm_profiles(name, provider, base_url, model, effort, api_key_sealed, api_key_hint, options)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+llmProfileCols,
		in.Name, in.Provider, in.BaseURL, in.Model, in.Effort, sealed, secrets.Hint(in.APIKey), in.Options))
	return p, mapErr(err)
}

func (s *Store) GetLLMProfile(ctx context.Context, id int64) (LLMProfile, error) {
	p, err := one[LLMProfile](s.pool.Query(ctx, `SELECT `+llmProfileCols+` FROM llm_profiles WHERE id = $1`, id))
	return p, mapErr(err)
}

// UpdateLLMProfile applies p. Replacing the key re-seals it; the old key is
// gone.
func (s *Store) UpdateLLMProfile(ctx context.Context, id int64, p LLMProfilePatch) (LLMProfile, error) {
	var sealed []byte
	hint := ""
	if p.APIKey != nil {
		var err error
		if sealed, err = s.sealer.Seal([]byte(*p.APIKey), llmKeyAAD); err != nil {
			return LLMProfile{}, err
		}
		hint = secrets.Hint(*p.APIKey)
	}
	out, err := one[LLMProfile](s.pool.Query(ctx, `
		UPDATE llm_profiles SET
			name     = COALESCE($2, name),
			base_url = COALESCE($3, base_url),
			model    = COALESCE($4, model),
			effort   = COALESCE($5, effort),
			api_key_sealed = CASE WHEN $6::bytea IS NULL THEN api_key_sealed ELSE $6 END,
			api_key_hint   = CASE WHEN $6::bytea IS NULL THEN api_key_hint ELSE $7 END,
			options  = COALESCE($8, options),
			updated_at = now()
		WHERE id = $1
		RETURNING `+llmProfileCols, id, p.Name, p.BaseURL, p.Model, p.Effort, sealed, hint, p.Options))
	return out, mapErr(err)
}

// DeleteLLMProfile removes a profile that no project, agent or unfinished
// run uses.
func (s *Store) DeleteLLMProfile(ctx context.Context, id int64) error {
	var projects, agents, runs int
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM projects WHERE llm_profile_id = $1 AND status <> 'archived'),
		       (SELECT count(*) FROM agent_profiles WHERE llm_profile_id = $1),
		       (SELECT count(*) FROM runs WHERE llm_profile_id = $1
		          AND status NOT IN ('completed','cancelled','failed','timed_out'))`, id).Scan(&projects, &agents, &runs)
	if err != nil {
		return err
	}
	if projects+agents+runs > 0 {
		return fmt.Errorf("%w: the profile is in use by %d project(s), %d agent(s) and %d unfinished run(s)", ErrConflict, projects, agents, runs)
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM llm_profiles WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) ListLLMProfiles(ctx context.Context) ([]LLMProfile, error) {
	return collect[LLMProfile](s.pool.Query(ctx, `SELECT `+llmProfileCols+` FROM llm_profiles ORDER BY name`))
}

// LLMCredentials returns a profile with its decrypted key, for building a
// model client. It must never be serialised to an API response.
func (s *Store) LLMCredentials(ctx context.Context, id int64) (llm.Profile, error) {
	var p llm.Profile
	var sealed []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, provider, base_url, model, effort, api_key_sealed, options FROM llm_profiles WHERE id = $1`, id).
		Scan(&p.ID, &p.Provider, &p.BaseURL, &p.Model, &p.Effort, &sealed, &p.Options)
	if err != nil {
		return llm.Profile{}, mapErr(err)
	}
	if sealed == nil {
		return llm.Profile{}, fmt.Errorf("%w: llm profile %d has no API key", ErrInvalid, id)
	}
	key, err := s.sealer.Open(sealed, llmKeyAAD)
	if err != nil {
		return llm.Profile{}, fmt.Errorf("llm profile %d: cannot decrypt key (wrong REMNANT_MASTER_KEY?): %w", id, err)
	}
	p.APIKey = string(key)
	return p, nil
}
