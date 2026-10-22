package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"logintoreadnormal/internal/db"
	"logintoreadnormal/internal/models"
)

const pasteCols = `p.id, p.author_id, a.username, p.title, p.body, p.audience, p.is_deleted, p.deleted_at, p.created_at`

// visibilityWhere is the SQL predicate selecting the pastes a user may see:
// their own pastes, plus the admin's pastes addressed to "all" or to them
// specifically. The admin himself is handled by a separate query.
const visibilityWhere = `(
        p.author_id = ?
     OR (p.author_id = ? AND p.audience = 'all')
     OR (p.author_id = ? AND p.audience = 'specific'
         AND EXISTS (SELECT 1 FROM paste_recipients r WHERE r.paste_id = p.id AND r.actor_id = ?))
)`

// CreatePaste inserts a paste and its recipients (for audience "specific").
func (r *Repo) CreatePaste(ctx context.Context, authorID int64, title, body, audience string, recipients []int64, now time.Time) (int64, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
        INSERT INTO pastes (author_id, title, body, audience, is_deleted, created_at)
        VALUES (?, ?, ?, ?, 0, ?)`,
		authorID, title, body, audience, models.FormatUTC(now))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if audience == "specific" {
		for _, uid := range recipients {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO paste_recipients (paste_id, actor_id) VALUES (?, ?)`,
				id, uid); err != nil {
				return 0, err
			}
		}
	}
	return id, tx.Commit()
}

// ListAllPastes returns every paste, newest first (admin view).
func (r *Repo) ListAllPastes(ctx context.Context) ([]models.Paste, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+pasteCols+`
        FROM pastes p JOIN actors a ON a.id = p.author_id
       ORDER BY p.created_at DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectPastes(rows)
}

// ListPastesForUser returns the pastes visible to a user, newest first.
func (r *Repo) ListPastesForUser(ctx context.Context, userID, adminID int64) ([]models.Paste, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+pasteCols+`
        FROM pastes p JOIN actors a ON a.id = p.author_id
       WHERE `+visibilityWhere+`
       ORDER BY p.created_at DESC, p.id DESC`,
		userID, adminID, adminID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectPastes(rows)
}

// CountNewPastesForUser counts visible pastes created at/after the given time
// (the "new since first visit" counter, requirement 14).
func (r *Repo) CountNewPastesForUser(ctx context.Context, userID, adminID int64, since time.Time) (int, error) {
	var n int
	err := r.DB.QueryRowContext(ctx, `SELECT COUNT(1)
        FROM pastes p
       WHERE `+visibilityWhere+` AND p.created_at >= ?`,
		userID, adminID, adminID, userID, models.FormatUTC(since)).Scan(&n)
	return n, err
}

// PasteByID loads a single paste (author name included).
func (r *Repo) PasteByID(ctx context.Context, id int64) (*models.Paste, error) {
	row := r.DB.QueryRowContext(ctx, `SELECT `+pasteCols+`
        FROM pastes p JOIN actors a ON a.id = p.author_id WHERE p.id = ?`, id)
	p, err := scanPaste(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, db.ErrNotFound
	}
	return p, err
}

// CanView reports whether actor may see the given paste (requirement 9/11).
func (r *Repo) CanView(ctx context.Context, actor *models.Actor, adminID int64, p *models.Paste) (bool, error) {
	if actor.IsAdmin() {
		return true, nil
	}
	if p.AuthorID == actor.ID {
		return true, nil
	}
	if p.AuthorID == adminID {
		switch p.Audience {
		case "all":
			return true, nil
		case "specific":
			var n int
			err := r.DB.QueryRowContext(ctx,
				`SELECT COUNT(1) FROM paste_recipients WHERE paste_id = ? AND actor_id = ?`,
				p.ID, actor.ID).Scan(&n)
			return n > 0, err
		}
	}
	return false, nil
}

// RecipientsForPastes returns the recipient logins grouped by paste id, ordered
// by login. Only "specific" pastes have recipients; unknown ids are absent.
func (r *Repo) RecipientsForPastes(ctx context.Context, pasteIDs []int64) (map[int64][]string, error) {
	out := make(map[int64][]string)
	if len(pasteIDs) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(pasteIDs)), ",")
	args := make([]any, 0, len(pasteIDs))
	for _, id := range pasteIDs {
		args = append(args, id)
	}

	rows, err := r.DB.QueryContext(ctx, `
        SELECT r.paste_id, a.username
          FROM paste_recipients r
          JOIN actors a ON a.id = r.actor_id
         WHERE r.paste_id IN (`+placeholders+`)
         ORDER BY a.username ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			pasteID  int64
			username string
		)
		if err := rows.Scan(&pasteID, &username); err != nil {
			return nil, err
		}
		out[pasteID] = append(out[pasteID], username)
	}
	return out, rows.Err()
}

// SoftDeletePaste wipes the body but keeps the row (placeholder for viewers).
func (r *Repo) SoftDeletePaste(ctx context.Context, id int64, now time.Time) error {
	_, err := r.DB.ExecContext(ctx, `
        UPDATE pastes SET is_deleted = 1, deleted_at = ?, body = NULL WHERE id = ?`,
		models.FormatUTC(now), id)
	return err
}

func scanPaste(sc interface {
	Scan(dest ...any) error
}) (*models.Paste, error) {
	var (
		p         models.Paste
		body      sql.NullString
		isDeleted int
		deletedAt sql.NullString
		createdAt string
	)
	if err := sc.Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.Title, &body, &p.Audience, &isDeleted, &deletedAt, &createdAt); err != nil {
		return nil, err
	}
	p.Body = body
	p.IsDeleted = isDeleted == 1
	var err error
	if p.DeletedAt, err = parseNullTime(deletedAt); err != nil {
		return nil, err
	}
	if p.CreatedAt, err = models.ParseUTC(createdAt); err != nil {
		return nil, err
	}
	return &p, nil
}

func collectPastes(rows *sql.Rows) ([]models.Paste, error) {
	var out []models.Paste
	for rows.Next() {
		p, err := scanPaste(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
