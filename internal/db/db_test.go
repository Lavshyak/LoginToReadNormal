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

// TestMigrateOfLiveDatabaseSimulatesProductionUpgrade builds a database in the
// previous on-disk format (with data), runs Migrate and checks that:
//   - nothing is lost (all rows and values survive);
//   - a user who had already activated loses the deadline (becomes permanent);
//   - a user who has not activated yet keeps the activation window;
//   - the admin is untouched.
func TestMigrateOfLiveDatabaseSimulatesProductionUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.ExecContext(ctx, oldActorsSchema); err != nil {
		t.Fatal(err)
	}

	rows := []struct {
		username   string
		role       string
		firstSeen  string
		expiresAt  string
		primaryAct int
	}{
		{"admin", "admin", "", "", 0},                                          // activated admin
		{"notyet", "user", "", "2026-01-02T03:04:05Z", 1},                      // still has the window
		{"already", "user", "2025-12-01T00:00:00Z", "2026-01-02T03:04:05Z", 0}, // activated before the upgrade
	}
	for _, r := range rows {
		var firstSeen, expiresAt any
		if r.firstSeen != "" {
			firstSeen = r.firstSeen
		}
		if r.expiresAt != "" {
			expiresAt = r.expiresAt
		}
		if _, err := d.ExecContext(ctx, `
            INSERT INTO actors (username, role, created_at, first_seen_at, expires_at,
                                is_active, primary_password_hash, primary_active)
            VALUES (?, ?, '2025-11-01T00:00:00Z', ?, ?, 1, ?, ?)`,
			r.username, r.role, firstSeen, expiresAt, "hash-"+r.username, r.primaryAct); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// No data loss.
	var count int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(1) FROM actors`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(rows) {
		t.Fatalf("actors count = %d, want %d (data loss!)", count, len(rows))
	}
	var hash string
	if err := d.QueryRowContext(ctx, `SELECT primary_password_hash FROM actors WHERE username = 'already'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != "hash-already" {
		t.Fatalf("password hash changed: %q", hash)
	}

	expiresOf := func(username string) (string, bool) {
		t.Helper()
		var v sql.NullString
		if err := d.QueryRowContext(ctx, `SELECT expires_at FROM actors WHERE username = ?`, username).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v.String, v.Valid
	}

	// Already activated -> permanent (deadline cleared).
	if v, ok := expiresOf("already"); ok {
		t.Fatalf("activated user still has a deadline: %q (would be locked out)", v)
	}
	// Not activated yet -> window preserved untouched.
	if v, ok := expiresOf("notyet"); !ok || v != "2026-01-02T03:04:05Z" {
		t.Fatalf("pending user lost its activation window: %q (valid=%v)", v, ok)
	}
	// Admin untouched.
	if v, ok := expiresOf("admin"); ok {
		t.Fatalf("admin got a deadline: %q", v)
	}

	// Idempotent: running again changes nothing.
	if err := Migrate(ctx, d); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if v, ok := expiresOf("notyet"); !ok || v != "2026-01-02T03:04:05Z" {
		t.Fatalf("second migrate altered the pending user: %q", v)
	}
}
