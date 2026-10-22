package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"logintoreadnormal/internal/db"
	"logintoreadnormal/internal/models"
)

// CreateLink stores a new permanent link (token hash + password hash).
func (r *Repo) CreateLink(ctx context.Context, actorID int64, tokenHash, passwordHash string, now time.Time) (int64, error) {
	res, err := r.DB.ExecContext(ctx, `
        INSERT INTO permanent_links (actor_id, token_hash, password_hash, is_active, created_at)
        VALUES (?, ?, ?, 1, ?)`,
		actorID, tokenHash, passwordHash, models.FormatUTC(now))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Activate atomically issues a new permanent link/password, consumes the
// primary credentials and clears the activation deadline: from this moment the
// account is permanent (expires_at = NULL -> never expires). first_seen_at is
// recorded on first activation (counter baseline).
func (r *Repo) Activate(ctx context.Context, actorID int64, tokenHash, passwordHash string, now time.Time) (int64, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
        INSERT INTO permanent_links (actor_id, token_hash, password_hash, is_active, created_at)
        VALUES (?, ?, ?, 1, ?)`,
		actorID, tokenHash, passwordHash, models.FormatUTC(now))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE actors
           SET primary_active = 0,
               first_seen_at = COALESCE(first_seen_at, ?),
               expires_at = NULL
         WHERE id = ?`, models.FormatUTC(now), actorID); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// LinkByTokenHash resolves a permanent link by the SHA-256 of its raw token.
func (r *Repo) LinkByTokenHash(ctx context.Context, tokenHash string) (*models.Link, error) {
	var (
		l         models.Link
		createdAt string
		isActive  int
	)
	err := r.DB.QueryRowContext(ctx, `
        SELECT id, actor_id, token_hash, password_hash, is_active, created_at
          FROM permanent_links WHERE token_hash = ?`, tokenHash).
		Scan(&l.ID, &l.ActorID, &l.TokenHash, &l.PasswordHash, &isActive, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, db.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l.IsActive = isActive == 1
	if l.CreatedAt, err = models.ParseUTC(createdAt); err != nil {
		return nil, err
	}
	return &l, nil
}
