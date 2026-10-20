// Package repo is the data-access layer over SQLite.
package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"logintoreadnormal/internal/db"
	"logintoreadnormal/internal/models"
)

// Repo wraps the database handle.
type Repo struct{ DB *sql.DB }

// New returns a repository.
func New(d *sql.DB) *Repo { return &Repo{DB: d} }

func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := models.ParseUTC(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// --- Actors ---------------------------------------------------------------

const actorCols = `id, username, role, created_at, first_seen_at, last_seen_at, expires_at, is_active, primary_active`

func scanActor(sc interface {
	Scan(dest ...any) error
}) (*models.Actor, error) {
	a := models.Actor{}
	if err := scanActorInto(sc, &a, nil); err != nil {
		return nil, err
	}
	return &a, nil
}

// scanActorInto scans the actorCols (+ optional trailing extra column into
// extra) so the same field mapping is reused everywhere.
func scanActorInto(sc interface {
	Scan(dest ...any) error
}, a *models.Actor, extra any) error {
	var (
		createdAt  string
		firstSeen  sql.NullString
		lastSeen   sql.NullString
		expiresAt  sql.NullString
		isActive   int
		primaryAct int
	)
	dest := []any{&a.ID, &a.Username, &a.Role, &createdAt, &firstSeen, &lastSeen, &expiresAt, &isActive, &primaryAct}
	if extra != nil {
		dest = append(dest, extra)
	}
	if err := sc.Scan(dest...); err != nil {
		return err
	}
	t, err := models.ParseUTC(createdAt)
	if err != nil {
		return err
	}
	a.CreatedAt = t
	if a.FirstSeenAt, err = parseNullTime(firstSeen); err != nil {
		return err
	}
	if a.LastSeenAt, err = parseNullTime(lastSeen); err != nil {
		return err
	}
	if a.ExpiresAt, err = parseNullTime(expiresAt); err != nil {
		return err
	}
	a.IsActive = isActive == 1
	a.PrimaryActive = primaryAct == 1
	return nil
}

// ActorByUsername looks up an actor by login.
func (r *Repo) ActorByUsername(ctx context.Context, username string) (*models.Actor, error) {
	row := r.DB.QueryRowContext(ctx, `SELECT `+actorCols+` FROM actors WHERE username = ?`, username)
	a, err := scanActor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, db.ErrNotFound
	}
	return a, err
}

// PrimaryCredential returns the actor plus the stored primary password hash,
// used only to verify a primary login.
func (r *Repo) PrimaryCredential(ctx context.Context, username string) (*models.Actor, string, error) {
	row := r.DB.QueryRowContext(ctx,
		`SELECT `+actorCols+`, primary_password_hash FROM actors WHERE username = ?`, username)
	a := models.Actor{}
	var hash string
	err := scanActorInto(row, &a, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", db.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return &a, hash, nil
}

// ActorByID looks up an actor by id.
func (r *Repo) ActorByID(ctx context.Context, id int64) (*models.Actor, error) {
	row := r.DB.QueryRowContext(ctx, `SELECT `+actorCols+` FROM actors WHERE id = ?`, id)
	a, err := scanActor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, db.ErrNotFound
	}
	return a, err
}

// AdminID returns the id of the (single) administrator.
func (r *Repo) AdminID(ctx context.Context) (int64, error) {
	var id int64
	err := r.DB.QueryRowContext(ctx,
		`SELECT id FROM actors WHERE role = 'admin' ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, db.ErrNotFound
	}
	return id, err
}

// CreateUser inserts a new user with a primary password and optional deadline.
func (r *Repo) CreateUser(ctx context.Context, username, primaryHash string, expiresAt *time.Time) (int64, error) {
	var exp any
	if expiresAt != nil {
		exp = models.FormatUTC(*expiresAt)
	}
	res, err := r.DB.ExecContext(ctx, `
        INSERT INTO actors (username, role, created_at, expires_at, is_active, primary_password_hash, primary_active)
        VALUES (?, 'user', ?, ?, 1, ?, 1)`,
		username, models.FormatUTC(time.Now()), exp, primaryHash)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// MarkSeen records the start of the current visit; the next visit's counter
// counts from this moment.
func (r *Repo) MarkSeen(ctx context.Context, actorID int64, now time.Time) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE actors SET last_seen_at = ? WHERE id = ?`, models.FormatUTC(now), actorID)
	return err
}

// DeleteUser removes a user account: it is deactivated, its permanent links are
// revoked and it disappears from the users list. The row is kept so that pastes
// submitted by the user (visible to the admin) keep their author.
func (r *Repo) DeleteUser(ctx context.Context, actorID int64, now time.Time) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
        UPDATE actors
           SET is_active = 0, primary_active = 0, deleted_at = ?
         WHERE id = ? AND role = 'user' AND deleted_at IS NULL`, models.FormatUTC(now), actorID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return db.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE permanent_links SET is_active = 0 WHERE actor_id = ?`, actorID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListActiveUsers returns all active, non-expired users (for audience pickers).
func (r *Repo) ListActiveUsers(ctx context.Context, now time.Time) ([]models.Actor, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+actorCols+`
        FROM actors
       WHERE role = 'user' AND is_active = 1 AND deleted_at IS NULL
         AND (expires_at IS NULL OR expires_at > ?)
       ORDER BY username ASC`, models.FormatUTC(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectActors(rows)
}

// ListUsers returns every live user (any status) for the admin users page.
func (r *Repo) ListUsers(ctx context.Context) ([]models.Actor, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+actorCols+`
        FROM actors WHERE role = 'user' AND deleted_at IS NULL ORDER BY username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectActors(rows)
}

func collectActors(rows *sql.Rows) ([]models.Actor, error) {
	var out []models.Actor
	for rows.Next() {
		a, err := scanActor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// UsernameExists reports whether a login is already taken.
func (r *Repo) UsernameExists(ctx context.Context, username string) (bool, error) {
	var n int
	err := r.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM actors WHERE username = ?`, username).Scan(&n)
	return n > 0, err
}
