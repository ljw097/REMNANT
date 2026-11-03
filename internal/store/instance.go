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

// Instance profiles. A profile presets the policy rules; any rule can then
// be changed, which makes the profile "custom".
const (
	// ProfilePersonal: one researcher on their own machine. They may approve
	// their own lab requests and review their own patterns; each such
	// decision is recorded as a self-decision.
	ProfilePersonal = "personal"
	// ProfileTeam: researchers cross-check each other. Nobody decides their
	// own requests.
	ProfileTeam = "team"
	// ProfileManaged: an organization where administrators gate things:
	// only an administrator approves lab experiments, and people may ask
	// for an account, which an administrator approves.
	ProfileManaged = "managed"
	ProfileCustom  = "custom"
)

// Policy is how the instance is used. It is stored as one instance setting.
type Policy struct {
	Profile string `json:"profile"`
	// SelfApproval lets the requester of a lab approval decide it.
	SelfApproval bool `json:"self_approval"`
	// SelfReview lets a pattern's author review it.
	SelfReview bool `json:"self_review"`
	// AdminLabApproval reserves lab approval decisions for administrators.
	AdminLabApproval bool `json:"admin_lab_approval"`
	// AccountRequests lets people request an account on the sign-in page;
	// an administrator approves each request.
	AccountRequests bool `json:"account_requests"`
	// SessionHours signs a browser session out after this long without
	// activity (1–720).
	SessionHours int `json:"session_hours"`
}

// Presets maps each profile except custom to its rules.
var Presets = map[string]Policy{
	ProfilePersonal: {Profile: ProfilePersonal, SelfApproval: true, SelfReview: true, SessionHours: 720},
	ProfileTeam:     {Profile: ProfileTeam, SessionHours: 168},
	ProfileManaged:  {Profile: ProfileManaged, AdminLabApproval: true, AccountRequests: true, SessionHours: 12},
}

const (
	policyKey    = "policy"
	setupCodeKey = "setup_code"
	// maxSetupAttempts wrong codes invalidate the setup code; a restart
	// issues a new one.
	maxSetupAttempts = 10
)

// Normalize validates p and names its profile: a profile whose rules no
// longer match its preset becomes custom.
func (p Policy) Normalize() (Policy, error) {
	if p.SessionHours < 1 || p.SessionHours > 720 {
		return Policy{}, fmt.Errorf("%w: session_hours must be 1–720", ErrInvalid)
	}
	if _, ok := Presets[p.Profile]; !ok && p.Profile != ProfileCustom {
		return Policy{}, fmt.Errorf("%w: profile must be personal, team, managed or custom", ErrInvalid)
	}
	if preset, ok := Presets[p.Profile]; !ok || preset != p {
		p.Profile = ProfileCustom
	}
	return p, nil
}

// Policy returns the instance policy; an instance without one is a team.
func (s *Store) Policy(ctx context.Context) (Policy, error) {
	var p Policy
	ok, err := s.Setting(ctx, policyKey, &p)
	if err != nil {
		return Policy{}, err
	}
	if !ok {
		return Presets[ProfileTeam], nil
	}
	if p.SessionHours < 1 || p.SessionHours > 720 {
		p.SessionHours = Presets[ProfileTeam].SessionHours
	}
	return p, nil
}

func (s *Store) SetPolicy(ctx context.Context, p Policy, by int64) (Policy, error) {
	p, err := p.Normalize()
	if err != nil {
		return Policy{}, err
	}
	return p, s.SetSetting(ctx, policyKey, p, by)
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
	Policy   Policy
	Username string
	// PasswordHash is the administrator's password hash. MustChange makes
	// them replace it at the first sign-in (the CLI generated it).
	PasswordHash string
	MustChange   bool
	Code         string // required unless TrustedLocal
	// TrustedLocal skips the code: the CLI runs on the server with database
	// access and needs no proof of being the operator.
	TrustedLocal bool
}

// Setup creates the first administrator and records the instance policy. It
// succeeds once: a table lock makes concurrent attempts serialise, and any
// existing user ends setup for good.
func (s *Store) Setup(ctx context.Context, in SetupInput) (User, error) {
	pol, err := in.Policy.Normalize()
	if err != nil {
		return User{}, err
	}
	if in.PasswordHash == "" {
		return User{}, fmt.Errorf("%w: a password is required", ErrInvalid)
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
			INSERT INTO users(username, role, password_hash, must_change_password, password_changed_at)
			VALUES ($1, 'admin', $2, $3, now())
			RETURNING `+userCols, in.Username, in.PasswordHash, in.MustChange))
		if err != nil {
			return mapErr(err)
		}
		user = u
		if _, err := tx.Exec(ctx, `
			INSERT INTO instance_settings(key, value, updated_by) VALUES ($1, $2, $3)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			policyKey, pol, u.ID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM instance_settings WHERE key = $1`, setupCodeKey)
		return err
	})
	if err != nil {
		return User{}, err
	}
	if badCode {
		return User{}, ErrSetupCode
	}
	return user, nil
}

// UserPatch changes a user's role or disabled flag, or approves an account
// request (Approve).
type UserPatch struct {
	Role     *auth.Role
	Disabled *bool
	Approve  bool
}

// UpdateUser applies p, refusing to leave the instance without an active
// administrator.
func (s *Store) UpdateUser(ctx context.Context, id int64, p UserPatch) (UserRef, error) {
	var out UserRef
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		u, err := one[UserRef](tx.Query(ctx, `SELECT `+userRefCols+` FROM users WHERE id = $1`, id))
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
		pending := u.Pending && !p.Approve
		out, err = one[UserRef](tx.Query(ctx, `
			UPDATE users SET role = $2, disabled = $3, pending = $4 WHERE id = $1
			RETURNING `+userRefCols, id, role, disabled, pending))
		if err == nil && disabled {
			_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, id)
		}
		return err
	})
	return out, err
}
