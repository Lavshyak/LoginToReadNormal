// Package db opens the SQLite database, applies the schema and seeds the admin.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"logintoreadnormal/internal/auth"
	"logintoreadnormal/internal/models"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

const schema = `
CREATE TABLE IF NOT EXISTS actors (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT    NOT NULL UNIQUE,
    role                  TEXT    NOT NULL,
    created_at            TEXT    NOT NULL,
    first_seen_at         TEXT,
    last_seen_at          TEXT,
    expires_at            TEXT,
    is_active             INTEGER NOT NULL DEFAULT 1,
    primary_password_hash TEXT    NOT NULL,
    primary_active        INTEGER NOT NULL DEFAULT 1,
    deleted_at            TEXT
);

CREATE TABLE IF NOT EXISTS permanent_links (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id      INTEGER NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    token_hash    TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    is_active     INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS pastes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    author_id  INTEGER NOT NULL REFERENCES actors(id),
    title      TEXT    NOT NULL,
    body       TEXT,
    audience   TEXT    NOT NULL,
    is_deleted INTEGER NOT NULL DEFAULT 0,
    deleted_at TEXT,
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS paste_recipients (
    paste_id INTEGER NOT NULL REFERENCES pastes(id) ON DELETE CASCADE,
    actor_id INTEGER NOT NULL REFERENCES actors(id) ON DELETE CASCADE,
    PRIMARY KEY (paste_id, actor_id)
);

CREATE INDEX IF NOT EXISTS idx_actors_username    ON actors(username);
CREATE INDEX IF NOT EXISTS idx_links_token        ON permanent_links(token_hash);
CREATE INDEX IF NOT EXISTS idx_pastes_created     ON pastes(created_at);
CREATE INDEX IF NOT EXISTS idx_recipients_actor   ON paste_recipients(actor_id);
`

// Open opens (creating if needed) the SQLite database with WAL and a single
// connection, which keeps memory usage low and avoids write contention.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		path,
	)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	d.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Migrate applies the schema, the idempotent column additions needed for
// databases created by an earlier version, and the data fix-ups that keep an
// in-place upgrade safe. Nothing is dropped or recreated, so existing data is
// preserved.
func Migrate(ctx context.Context, d *sql.DB) error {
	if _, err := d.ExecContext(ctx, schema); err != nil {
		return err
	}
	// Columns added after the first release.
	for _, c := range []struct{ table, column, ddl string }{
		{"actors", "last_seen_at", "last_seen_at TEXT"},
		{"actors", "deleted_at", "deleted_at TEXT"},
	} {
		if err := ensureColumn(ctx, d, c.table, c.column, c.ddl); err != nil {
			return err
		}
	}

	// Behaviour change (safe, data-preserving):
	//   actors.expires_at used to mean the TOTAL account lifetime counted from
	//   the moment the admin created the user. It now means only the ACTIVATION
	//   WINDOW: a user must activate before this moment, and once activated the
	//   account is permanent (Activate sets expires_at = NULL).
	//   Clear the deadline for everyone who has already activated (first_seen_at
	//   is written exactly at activation), so upgrading an existing database
	//   never locks out users who were already active.
	if _, err := d.ExecContext(ctx, `
        UPDATE actors
           SET expires_at = NULL
         WHERE role = 'user'
           AND first_seen_at IS NOT NULL
           AND expires_at IS NOT NULL`); err != nil {
		return err
	}
	return nil
}

// ensureColumn adds a column when it does not exist yet. The connection pool is
// limited to a single connection, so the row set must be fully closed before
// the ALTER TABLE statement runs.
func ensureColumn(ctx context.Context, d *sql.DB, table, column, ddl string) error {
	rows, err := d.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	exists := false
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			exists = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if exists {
		return nil
	}
	_, err = d.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+ddl)
	return err
}

// SeedAdmin creates the administrator from the config on first start. It is a
// no-op when an actor with the given login already exists, so restarts never
// reset an activated admin.
func SeedAdmin(ctx context.Context, d *sql.DB, login, primaryPassword string) (created bool, err error) {
	var exists int
	err = d.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM actors WHERE username = ?`, login).Scan(&exists)
	if err != nil {
		return false, err
	}
	if exists > 0 {
		return false, nil
	}
	hash, err := auth.HashPassword(primaryPassword)
	if err != nil {
		return false, err
	}
	now := models.FormatUTC(time.Now())
	_, err = d.ExecContext(ctx, `
        INSERT INTO actors (username, role, created_at, is_active, primary_password_hash, primary_active)
        VALUES (?, 'admin', ?, 1, ?, 1)`, login, now, hash)
	if err != nil {
		return false, err
	}
	return true, nil
}

// ErrNotFound is returned by repository lookups when nothing matches.
var ErrNotFound = errors.New("not found")
