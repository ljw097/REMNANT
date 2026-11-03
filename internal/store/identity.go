package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ljw097/remnant/internal/auth"
)

// CreateUser adds a user who signs in with API tokens only.
func (s *Store) CreateUser(ctx context.Context, username string, role auth.Role) (User, error) {
	return one[User](s.pool.Query(ctx, `
		INSERT INTO users(username, role) VALUES ($1, $2)
		RETURNING `+userCols, username, string(role)))
}

// CreateUserWithPassword adds a user who signs in with a password.
// mustChange makes them replace it first (it was generated for them).
func (s *Store) CreateUserWithPassword(ctx context.Context, username string, role auth.Role, hash string, mustChange bool) (User, error) {
	return one[User](s.pool.Query(ctx, `
		INSERT INTO users(username, role, password_hash, must_change_password, password_changed_at)
		VALUES ($1, $2, $3, $4, now())
		RETURNING `+userCols, username, string(role), hash, mustChange))
}

// RequestAccount records an account request from the sign-in page: a
// researcher who cannot sign in until an administrator approves them.
func (s *Store) RequestAccount(ctx context.Context, username, hash string) (User, error) {
	return one[User](s.pool.Query(ctx, `
		INSERT INTO users(username, role, password_hash, pending, password_changed_at)
		VALUES ($1, 'researcher', $2, true, now())
		RETURNING `+userCols, username, hash))
}

// DeleteAccountRequest removes a pending account request.
func (s *Store) DeleteAccountRequest(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND pending`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return one[User](s.pool.Query(ctx, `SELECT `+userCols+` FROM users WHERE username = $1`, username))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return one[User](s.pool.Query(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// SetPassword replaces a user's password and signs out every session except
// keepSession (0 signs out all of them).
func (s *Store) SetPassword(ctx context.Context, userID int64, hash string, mustChange bool, keepSession int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE users SET password_hash = $2, must_change_password = $3, password_changed_at = now(),
			                 failed_logins = 0, locked_until = NULL
			WHERE id = $1`, userID, hash, mustChange)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`, userID, keepSession)
		return err
	})
}

// PasswordHash returns a user's password hash ("" when they have none).
func (s *Store) PasswordHash(ctx context.Context, userID int64) (string, error) {
	var h *string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil || h == nil {
		return "", err
	}
	return *h, nil
}

// Sign-in failures. ErrLogin covers an unknown user, a wrong password, a
// disabled account and an account without a password alike.
var (
	ErrLogin        = errors.New("wrong username or password")
	ErrLoginLocked  = errors.New("too many failed sign-ins; try again later")
	ErrLoginPending = errors.New("this account is waiting for an administrator's approval")
)

const (
	// maxLoginFailures wrong passwords in a row lock the account for
	// loginLockout.
	maxLoginFailures = 10
	loginLockout     = 15 * time.Minute
	// SessionMaxAge bounds a session however active it stays.
	SessionMaxAge = 30 * 24 * time.Hour
)

// Login checks a username and password and opens a browser session that
// ends after idle without activity. It returns the session token, shown to
// the browser once as a cookie.
func (s *Store) Login(ctx context.Context, username, password, userAgent string, idle time.Duration) (auth.Principal, string, error) {
	var (
		p                             auth.Principal
		role                          string
		hash                          *string
		disabled, pending, mustChange bool
		failed                        int
		lockedUntil                   *time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, role, password_hash, disabled, pending, must_change_password, failed_logins, locked_until
		FROM users WHERE username = $1`, username).
		Scan(&p.UserID, &p.Username, &role, &hash, &disabled, &pending, &mustChange, &failed, &lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		auth.VerifyPassword("", password)
		return auth.Principal{}, "", ErrLogin
	}
	if err != nil {
		return auth.Principal{}, "", err
	}
	encoded := ""
	if hash != nil {
		encoded = *hash
	}
	ok := auth.VerifyPassword(encoded, password)
	if lockedUntil != nil && time.Now().Before(*lockedUntil) {
		return auth.Principal{}, "", ErrLoginLocked
	}
	if !ok {
		// Count the failure; the tenth in a row locks the account.
		_, err := s.pool.Exec(ctx, `
			UPDATE users SET failed_logins = CASE WHEN failed_logins + 1 >= $2 THEN 0 ELSE failed_logins + 1 END,
			                 locked_until  = CASE WHEN failed_logins + 1 >= $2 THEN now() + $3::interval ELSE locked_until END
			WHERE id = $1`, p.UserID, maxLoginFailures, loginLockout.String())
		if err != nil {
			return auth.Principal{}, "", err
		}
		return auth.Principal{}, "", ErrLogin
	}
	if disabled {
		return auth.Principal{}, "", ErrLogin
	}
	if pending {
		return auth.Principal{}, "", ErrLoginPending
	}
	token, digest, err := auth.NewSessionToken()
	if err != nil {
		return auth.Principal{}, "", err
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET failed_logins = 0, locked_until = NULL WHERE id = $1`, p.UserID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO sessions(user_id, token_sha256, expires_at, idle_seconds, user_agent)
			VALUES ($1, $2, now() + $3::interval, $4, $5) RETURNING id`,
			p.UserID, digest, SessionMaxAge.String(), int(idle.Seconds()), userAgent).Scan(&p.SessionID)
	})
	if err != nil {
		return auth.Principal{}, "", err
	}
	p.Role, p.MustChangePassword = auth.Role(role), mustChange
	return p, token, nil
}

// LookupSession implements auth.TokenLookup for browser sessions. A session
// ends when revoked, past its maximum age, idle for too long, or when its
// user is disabled.
func (s *Store) LookupSession(ctx context.Context, digest string) (auth.Principal, error) {
	var p auth.Principal
	var role string
	err := s.pool.QueryRow(ctx, `
		UPDATE sessions t SET last_seen_at = now()
		FROM users u
		WHERE t.token_sha256 = $1 AND t.user_id = u.id
		  AND t.revoked_at IS NULL
		  AND t.expires_at > now()
		  AND t.last_seen_at + make_interval(secs => t.idle_seconds) > now()
		  AND NOT u.disabled AND NOT u.pending
		RETURNING u.id, u.username, u.role, u.must_change_password, t.id`, digest).
		Scan(&p.UserID, &p.Username, &role, &p.MustChangePassword, &p.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	p.Role = auth.Role(role)
	return p, err
}

// Logout revokes one session.
func (s *Store) Logout(ctx context.Context, sessionID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	return err
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
		  AND NOT u.disabled AND NOT u.pending
		RETURNING u.id, u.username, u.role`, digest).Scan(&p.UserID, &p.Username, &role)
	if err == pgx.ErrNoRows {
		return auth.Principal{}, auth.ErrInvalidToken
	}
	p.Role = auth.Role(role)
	return p, err
}
