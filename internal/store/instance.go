package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/auth"
)

// Deployment modes. Team mode enforces two-person rules; personal mode lets
// one person decide their own requests, recorded as self-approval.
const (
	ModeTeam     = "team"
	ModePersonal = "personal"
)

const (
	modeKey      = "deployment_mode"
	setupCodeKey = "setup_code"
	// maxSetupAttempts wrong codes invalidate the setup code; a restart
	// issues a new one.
	maxSetupAttempts = 10
)

// DeploymentMode returns team or personal; an unset mode is team.
func (s *Store) DeploymentMode(ctx context.Context) (string, error) {
	var mode string
	ok, err := s.Setting(ctx, modeKey, &mode)
	if err != nil {
		return "", err
	}
	if !ok || (mode != ModeTeam && mode != ModePersonal) {
		return ModeTeam, nil
	}
	return mode, nil
}

func (s *Store) SetDeploymentMode(ctx context.Context, mode string, by int64) error {
	if mode != ModeTeam && mode != ModePersonal {
		return fmt.Errorf("%w: mode must be team or personal", ErrInvalid)
	}
	return s.SetSetting(ctx, modeKey, mode, by)
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

type setupCode struct {
	SHA256    string    `json:"sha256"`
	ExpiresAt time.Time `json:"expires_at"`
	Failures  int       `json:"failures"`
}

// setupAlphabet leaves out characters that are easy to misread (0/O, 1/I/L).
const setupAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// NewSetupCode issues the one-time code that authorises first-run setup
// from the web. Only its digest is stored. It is replaced on every call.
func (s *Store) NewSetupCode(ctx context.Context, ttl time.Duration) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(setupAlphabet[int(v)%len(setupAlphabet)])
	}
	code := sb.String()
	err := s.SetSetting(ctx, setupCodeKey, setupCode{SHA256: digestCode(code), ExpiresAt: time.Now().Add(ttl)}, 0)
	return code, err
}

func digestCode(code string) string {
	norm := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte("remnant-setup:" + norm))
	return hex.EncodeToString(sum[:])
}

// ErrSetupDone means the instance already has users.
var ErrSetupDone = fmt.Errorf("%w: this instance is already set up", ErrConflict)

// ErrSetupCode means the setup code is wrong, expired or used up.
var ErrSetupCode = fmt.Errorf("%w: the setup code is wrong or expired", ErrForbidden)

// SetupInput describes the first administrator and how the instance is used.
type SetupInput struct {
	Mode     string
	Username string
	Code     string // required unless TrustedLocal
	// TrustedLocal skips the code: the CLI runs on the server with database
	// access and needs no proof of being the operator.
	TrustedLocal bool
}

// Setup creates the first administrator and records the deployment mode. It
// succeeds once: a table lock makes concurrent attempts serialise, and any
// existing user ends setup for good.
func (s *Store) Setup(ctx context.Context, in SetupInput) (User, string, error) {
	if in.Mode != ModeTeam && in.Mode != ModePersonal {
		return User{}, "", fmt.Errorf("%w: mode must be team or personal", ErrInvalid)
	}
	token, digest, err := auth.NewToken()
	if err != nil {
		return User{}, "", err
	}
	var user User
	var badCode bool
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		if !in.TrustedLocal {
			var raw []byte
			err := tx.QueryRow(ctx, `SELECT value FROM instance_settings WHERE key = $1 FOR UPDATE`, setupCodeKey).Scan(&raw)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSetupCode
			}
			if err != nil {
				return err
			}
			var sc setupCode
			if err := jsonUnmarshal(raw, &sc); err != nil {
				return err
			}
			ok := subtle.ConstantTimeCompare([]byte(sc.SHA256), []byte(digestCode(in.Code))) == 1
			if !ok || time.Now().After(sc.ExpiresAt) || sc.Failures >= maxSetupAttempts {
				badCode = true
				sc.Failures++
				if _, err := tx.Exec(ctx, `UPDATE instance_settings SET value = $2 WHERE key = $1`, setupCodeKey, sc); err != nil {
					return err
				}
				return nil // commit the failure count, then report it
			}
		}
		u, err := one[User](tx.Query(ctx, `
			INSERT INTO users(username, role) VALUES ($1, 'admin')
			RETURNING id, username, role, disabled, created_at`, in.Username))
		if err != nil {
			return mapErr(err)
		}
		user = u
		if _, err := tx.Exec(ctx, `INSERT INTO api_tokens(user_id, name, token_sha256) VALUES ($1, 'setup', $2)`, u.ID, digest); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO instance_settings(key, value, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			modeKey, `"`+in.Mode+`"`, u.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM instance_settings WHERE key = $1`, setupCodeKey)
		return err
	})
	if err != nil {
		return User{}, "", err
	}
	if badCode {
		return User{}, "", ErrSetupCode
	}
	return user, token, nil
}

// UserPatch changes a user's role or disabled flag.
type UserPatch struct {
	Role     *auth.Role
	Disabled *bool
}

// UpdateUser applies p, refusing to leave the instance without an active
// administrator.
func (s *Store) UpdateUser(ctx context.Context, id int64, p UserPatch) (UserRef, error) {
	var out UserRef
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		u, err := one[UserRef](tx.Query(ctx, `SELECT id, username, role, disabled FROM users WHERE id = $1`, id))
		if err != nil {
			return err
		}
		role, disabled := u.Role, u.Disabled
		if p.Role != nil {
			role = string(*p.Role)
		}
		if p.Disabled != nil {
			disabled = *p.Disabled
		}
		if u.Role == "admin" && !u.Disabled && (role != "admin" || disabled) {
			var others int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND NOT disabled AND id <> $1`, id).Scan(&others); err != nil {
				return err
			}
			if others == 0 {
				return fmt.Errorf("%w: the instance needs at least one active administrator", ErrConflict)
			}
		}
		out, err = one[UserRef](tx.Query(ctx, `
			UPDATE users SET role = $2, disabled = $3 WHERE id = $1
			RETURNING id, username, role, disabled`, id, role, disabled))
		return err
	})
	return out, err
}
