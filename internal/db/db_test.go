package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// oldActorsSchema mirrors the actors table as it was before last_seen_at and
// deleted_at were introduced.
const oldActorsSchema = `
CREATE TABLE actors (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT    NOT NULL UNIQUE,
    role                  TEXT    NOT NULL,
    created_at            TEXT    NOT NULL,
    first_seen_at         TEXT,
    expires_at            TEXT,
    is_active             INTEGER NOT NULL DEFAULT 1,
    primary_password_hash TEXT    NOT NULL,
    primary_active        INTEGER NOT NULL DEFAULT 1
);`

func columnExists(t *testing.T, d *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := d.QueryContext(context.Background(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestMigrateAddsColumnsForExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Simulate a database created by the previous release.
	if _, err := d.ExecContext(ctx, oldActorsSchema); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, col := range []string{"first_seen_at", "last_seen_at", "expires_at", "deleted_at"} {
		if !columnExists(t, d, "actors", col) {
			t.Errorf("actors.%s is missing after Migrate", col)
		}
	}
	// Migrate must be idempotent.
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestSeedAdminIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}

	created, err := SeedAdmin(ctx, d, "admin", "primary-password")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first SeedAdmin did not create the admin")
	}
	created, err = SeedAdmin(ctx, d, "admin", "other-password")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second SeedAdmin recreated the admin")
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(1) FROM actors WHERE username = 'admin'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected exactly one admin, got %d", n)
	}
}
