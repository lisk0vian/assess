// Package sqlite implements session.Store on SQLite (modernc.org/sqlite, no
// CGO). Writes go through a single connection so they are serialized; reads
// use a separate pool and never block on writers thanks to WAL mode.
//
// In an encrypted vault the sensitive columns are sealed (package seal):
// published content and assets, names and identifiers, answers, event data
// and notes. Everything else (codes, times, states, scores) stays plain so
// the database can be queried without the key.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/lisk0vian/assess/internal/seal"
	"github.com/lisk0vian/assess/internal/session"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DB is a SQLite-backed session.Store.
type DB struct {
	w *sql.DB // single connection: the only writer
	r *sql.DB // read-only pool

	cipher *seal.Cipher // nil: values are written plain
	locked bool         // encrypted vault without its key: sealed values cannot be read or written
}

// Option configures Open.
type Option func(*DB)

// WithCipher seals sensitive values with c. A nil c with encrypted set
// opens the database locked: plain columns work, sealed ones fail with
// seal.ErrLocked and nothing sensitive is ever written in the clear.
func WithCipher(c *seal.Cipher, encrypted bool) Option {
	return func(db *DB) { db.cipher, db.locked = c, encrypted && c == nil }
}

var _ session.Store = (*DB)(nil)

// Open opens (creating if needed) the database file at path and applies
// pending migrations.
func Open(ctx context.Context, path string, opts ...Option) (*DB, error) {
	dsn := "file:" + filepath.ToSlash(path) + "?" + strings.Join([]string{
		"_pragma=foreign_keys(1)",
		"_pragma=secure_delete(1)", // deleted values (a forgotten student) are overwritten
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=busy_timeout(10000)",
		"_txlock=immediate",
	}, "&")
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxIdleTime(0)

	db := &DB{w: w}
	for _, o := range opts {
		o(db)
	}
	if err := db.migrate(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}

	r, err := sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(max(4, runtime.NumCPU()))
	db.r = r
	return db, nil
}

// Close closes both pools.
func (db *DB) Close() error {
	return errors.Join(db.r.Close(), db.w.Close())
}

// Update runs fn in a serialized write transaction.
func (db *DB) Update(ctx context.Context, fn func(session.Tx) error) error {
	return db.run(ctx, db.w, nil, fn)
}

// View runs fn in a read-only transaction with a consistent snapshot.
func (db *DB) View(ctx context.Context, fn func(session.Tx) error) error {
	return db.run(ctx, db.r, &sql.TxOptions{ReadOnly: true}, fn)
}

func (db *DB) run(ctx context.Context, pool *sql.DB, opts *sql.TxOptions, fn func(session.Tx) error) (err error) {
	sqlTx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			sqlTx.Rollback()
			panic(p)
		}
	}()
	if err := fn(&tx{ctx: ctx, tx: sqlTx, c: db.cipher, locked: db.locked}); err != nil {
		sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}

// migrate applies embedded migrations named NNNN_description.sql in order.
func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.w.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return err
	}
	var current int
	if err := db.w.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}

	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	latest := 0
	for _, name := range names {
		base := filepath.Base(name)
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %s", base)
		}
		latest = max(latest, version)
		if version <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		t, err := db.w.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := t.ExecContext(ctx, string(body)); err != nil {
			t.Rollback()
			return fmt.Errorf("%s: %w", base, err)
		}
		if _, err := t.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, time.Now().UnixMilli()); err != nil {
			t.Rollback()
			return err
		}
		if err := t.Commit(); err != nil {
			return err
		}
	}
	if current > latest {
		return fmt.Errorf("database schema version %d is newer than this ax (%d); upgrade ax", current, latest)
	}
	return nil
}
