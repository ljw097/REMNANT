package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/auth"
)

func (s *Store) CreateUser(ctx context.Context, username string, role auth.Role) (User, error) {
	return one[User](s.pool.Query(ctx, `
		INSERT INTO users(username, role) VALUES ($1, $2)
		RETURNING id, username, role, disabled, created_at`, username, string(role)))
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return one[User](s.pool.Query(ctx, `
		SELECT id, username, role, disabled, created_at FROM users WHERE username = $1`, username))
}

// CreateToken issues a bearer token for userID. The plaintext is returned
// once and never stored. ttl <= 0 means no expiry.
func (s *Store) CreateToken(ctx context.Context, userID int64, name string, ttl time.Duration) (string, error) {
	token, digest, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	var expires *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expires = &t
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO api_tokens(user_id, name, token_sha256, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, name, digest, expires)
	if err != nil {
		return "", mapErr(err)
	}
	return token, nil
}

// LookupToken implements auth.TokenLookup.
func (s *Store) LookupToken(ctx context.Context, digest string) (auth.Principal, error) {
	var p auth.Principal
	var role string
	err := s.pool.QueryRow(ctx, `
		UPDATE api_tokens t SET last_used_at = now()
		FROM users u
		WHERE t.token_sha256 = $1 AND t.user_id = u.id
		  AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > now())
		  AND NOT u.disabled
		RETURNING u.id, u.username, u.role`, digest).Scan(&p.UserID, &p.Username, &role)
	if err == pgx.ErrNoRows {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	p.Role = auth.Role(role)
	return p, err
}
