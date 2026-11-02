// Package store is REMNANT's PostgreSQL persistence layer. All tables live
// in the "remnant" schema, created and versioned by Migrate.
package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ljw097/remnant/internal/secrets"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrConflict signals a state precondition failure (wrong status, stale
	// approval, duplicate key).
	ErrConflict = errors.New("conflict")
	// ErrInvalid signals input that references unusable records.
	ErrInvalid = errors.New("invalid")
	// ErrForbidden signals an action the project policy or separation-of-duty
	// rules do not allow.
	ErrForbidden = errors.New("forbidden")
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool   *pgxpool.Pool
	sealer *secrets.Sealer
}

// Open connects with search_path pinned to the remnant schema.
func Open(ctx context.Context, databaseURL string, sealer *secrets.Sealer) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "remnant"
	cfg.ConnConfig.RuntimeParams["application_name"] = "remnantd"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, sealer: sealer}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies pending migrations in lexical order, each in its own
// transaction, under an advisory lock so concurrent starts are safe. An
// applied migration whose file has since changed is an error.
func (s *Store) Migrate(ctx context.Context) ([]string, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	const lockKey = 0x52454d4e414e54 // "REMNANT"
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return nil, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey) //nolint:errcheck

	if _, err := conn.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS remnant;
		CREATE TABLE IF NOT EXISTS remnant.schema_migrations (
			version    TEXT PRIMARY KEY,
			sha256     TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, err
	}

	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	var applied []string
	for _, name := range names {
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return applied, err
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		version := name[len("migrations/"):]

		var existing string
		err = conn.QueryRow(ctx, `SELECT sha256 FROM remnant.schema_migrations WHERE version = $1`, version).Scan(&existing)
		switch {
		case err == nil:
			if existing != digest {
				return applied, fmt.Errorf("migration %s was modified after it was applied", version)
			}
			continue
		case !errors.Is(err, pgx.ErrNoRows):
			return applied, err
		}

		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO remnant.schema_migrations(version, sha256) VALUES ($1, $2)`, version, digest)
			return err
		})
		if err != nil {
			return applied, fmt.Errorf("migration %s: %w", version, err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

// mapErr converts driver errors into the package's sentinel errors.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", ErrConflict, pgErr.Detail)
		case "23503", "23514": // foreign_key_violation, check_violation
			return fmt.Errorf("%w: %s", ErrConflict, pgErr.Message)
		}
	}
	return err
}

func collect[T any](rows pgx.Rows, err error) ([]T, error) {
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if out == nil {
		out = []T{}
	}
	return out, err
}

func one[T any](rows pgx.Rows, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, err
	}
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
	return v, mapErr(err)
}

func jsonUnmarshal(b []byte, v any) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}
