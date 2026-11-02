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
	api_key_sealed IS NOT NULL AS has_api_key, api_key_hint, updated_at`

type LLMProfileInput struct {
	Name     string
	Provider string
	BaseURL  string
	Model    string
	Effort   string
	APIKey   string
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
		INSERT INTO llm_profiles(name, provider, base_url, model, effort, api_key_sealed, api_key_hint)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+llmProfileCols,
		in.Name, in.Provider, in.BaseURL, in.Model, in.Effort, sealed, secrets.Hint(in.APIKey)))
	return p, mapErr(err)
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
		SELECT id, provider, base_url, model, effort, api_key_sealed FROM llm_profiles WHERE id = $1`, id).
		Scan(&p.ID, &p.Provider, &p.BaseURL, &p.Model, &p.Effort, &sealed)
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
