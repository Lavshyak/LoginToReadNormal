package models

import (
	"database/sql"
	"time"
)

// Actor is an admin or a user.
type Actor struct {
	ID            int64
	Username      string
	Role          string // "admin" | "user"
	CreatedAt     time.Time
	FirstSeenAt   *time.Time // first activation (baseline of the very first visit)
	LastSeenAt    *time.Time // start of the previous visit (counter baseline)
	ExpiresAt     *time.Time // deadline for activation; nil = account is permanent (already activated or no deadline)
	IsActive      bool
	PrimaryActive bool // primary login/password still valid
}

// CounterBaseline returns the moment the "new pastes" counter counts from:
// the previous visit if there was one, otherwise the activation moment.
func (a *Actor) CounterBaseline() *time.Time {
	if a.LastSeenAt != nil {
		return a.LastSeenAt
	}
	return a.FirstSeenAt
}

// IsAdmin reports whether the actor is an administrator.
func (a *Actor) IsAdmin() bool { return a.Role == "admin" }

// Expired reports whether the actor's activation deadline has passed. A nil
// deadline means "never expires" (an activated account is permanent).
func (a *Actor) Expired(now time.Time) bool {
	return a.ExpiresAt != nil && now.After(*a.ExpiresAt)
}

// Link is a permanent random link plus its permanent password.
type Link struct {
	ID           int64
	ActorID      int64
	TokenHash    string // SHA-256 of the raw token
	PasswordHash string // bcrypt
	IsActive     bool
	CreatedAt    time.Time
}

// Paste is a raw-text paste.
type Paste struct {
	ID         int64
	AuthorID   int64
	AuthorName string
	Title      string
	Body       sql.NullString // NULL after soft-delete
	Audience   string         // "all" | "specific" | "admin_only"
	IsDeleted  bool
	DeletedAt  *time.Time
	CreatedAt  time.Time
}
