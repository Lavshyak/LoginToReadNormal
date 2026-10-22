package repo

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"logintoreadnormal/internal/db"
	"logintoreadnormal/internal/models"
)

func newTestRepo(t *testing.T) (*Repo, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	ctx := context.Background()
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SeedAdmin(ctx, d, "admin", "primary"); err != nil {
		t.Fatal(err)
	}
	r := New(d)
	adminID, err := r.AdminID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r, adminID
}

func titles(pastes []models.Paste) []string {
	out := make([]string, 0, len(pastes))
	for _, p := range pastes {
		out = append(out, p.Title)
	}
	return out
}

func TestPasteVisibility(t *testing.T) {
	ctx := context.Background()
	r, adminID := newTestRepo(t)
	now := time.Now().UTC()

	alice, err := r.CreateUser(ctx, "alice", "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := r.CreateUser(ctx, "bob", "h", nil)
	if err != nil {
		t.Fatal(err)
	}

	add := func(author int64, title, audience string, recipients []int64) {
		t.Helper()
		if _, err := r.CreatePaste(ctx, author, title, "body-"+title, audience, recipients, now); err != nil {
			t.Fatal(err)
		}
	}
	add(adminID, "admin-all", "all", nil)
	add(adminID, "admin-alice", "specific", []int64{alice})
	add(adminID, "admin-nobody", "specific", nil)
	add(alice, "alice-paste", "admin_only", nil)

	all, err := r.ListAllPastes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("admin sees %v, want all 4", titles(all))
	}

	alicePastes, err := r.ListPastesForUser(ctx, alice, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(alicePastes); len(got) != 3 {
		t.Fatalf("alice sees %v, want 3 (admin-all, admin-alice, alice-paste)", got)
	}
	for _, p := range alicePastes {
		if p.Title == "admin-nobody" {
			t.Fatal("alice must not see an admin paste addressed to nobody")
		}
	}

	bobPastes, err := r.ListPastesForUser(ctx, bob, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(bobPastes); len(got) != 1 || got[0] != "admin-all" {
		t.Fatalf("bob sees %v, want only admin-all", got)
	}

	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if cur.CreatedAt.After(prev.CreatedAt) {
			t.Fatalf("pastes are not ordered newest-first: %v", titles(all))
		}
		if cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID > prev.ID {
			t.Fatalf("equal timestamps are not ordered newest-first: %v", titles(all))
		}
	}
}

func TestPastesOrderedNewestFirst(t *testing.T) {
	ctx := context.Background()
	r, adminID := newTestRepo(t)
	base := time.Now().UTC()

	alice, err := r.CreateUser(ctx, "alice", "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Created in chronological order: oldest -> newest.
	for i, title := range []string{"oldest", "middle", "newest"} {
		if _, err := r.CreatePaste(ctx, adminID, title, "b", "all", nil, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"newest", "middle", "oldest"}

	all, err := r.ListAllPastes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(all); !equalSlices(got, want) {
		t.Fatalf("admin list = %v, want %v (newest first)", got, want)
	}

	userPastes, err := r.ListPastesForUser(ctx, alice, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(userPastes); !equalSlices(got, want) {
		t.Fatalf("user list = %v, want %v (newest first)", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCanView(t *testing.T) {
	ctx := context.Background()
	r, adminID := newTestRepo(t)
	now := time.Now().UTC()

	alice, _ := r.CreateUser(ctx, "alice", "h", nil)
	bob, _ := r.CreateUser(ctx, "bob", "h", nil)
	admin, err := r.ActorByID(ctx, adminID)
	if err != nil {
		t.Fatal(err)
	}
	aliceActor, _ := r.ActorByID(ctx, alice)
	bobActor, _ := r.ActorByID(ctx, bob)

	specific, err := r.CreatePaste(ctx, adminID, "to-alice", "b", "specific", []int64{alice}, now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.PasteByID(ctx, specific)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		actor *models.Actor
		want  bool
	}{
		{"admin", admin, true},
		{"recipient", aliceActor, true},
		{"other user", bobActor, false},
	} {
		got, err := r.CanView(ctx, tc.actor, adminID, p)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("CanView(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCounterSinceGivenMoment(t *testing.T) {
	ctx := context.Background()
	r, adminID := newTestRepo(t)
	base := time.Now().UTC()

	alice, err := r.CreateUser(ctx, "alice", "h", nil)
	if err != nil {
		t.Fatal(err)
	}

	n, err := r.CountNewPastesForUser(ctx, alice, adminID, base)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("counter = %d, want 0 before any paste", n)
	}

	if _, err := r.CreatePaste(ctx, adminID, "p1", "b", "all", nil, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreatePaste(ctx, adminID, "p2", "b", "all", nil, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if n, err = r.CountNewPastesForUser(ctx, alice, adminID, base); err != nil || n != 2 {
		t.Fatalf("counter since base = %d (err %v), want 2", n, err)
	}
	// Counting from a later moment (a later visit) sees fewer pastes.
	if n, err = r.CountNewPastesForUser(ctx, alice, adminID, base.Add(90*time.Second)); err != nil || n != 1 {
		t.Fatalf("counter since base+90s = %d (err %v), want 1", n, err)
	}
	// Pastes invisible to the user are never counted.
	if _, err := r.CreatePaste(ctx, adminID, "p3", "b", "specific", nil, base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n, err = r.CountNewPastesForUser(ctx, alice, adminID, base); err != nil || n != 2 {
		t.Fatalf("counter after invisible paste = %d (err %v), want 2", n, err)
	}
}

func TestMarkSeenMovesBaseline(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRepo(t)
	alice, err := r.CreateUser(ctx, "alice", "h", nil)
	if err != nil {
		t.Fatal(err)
	}

	a, err := r.ActorByID(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if a.LastSeenAt != nil {
		t.Fatal("last_seen_at should be empty before the first visit")
	}

	t1 := time.Now().UTC().Truncate(time.Second)
	if err := r.MarkSeen(ctx, alice, t1); err != nil {
		t.Fatal(err)
	}
	a, _ = r.ActorByID(ctx, alice)
	if a.LastSeenAt == nil || !a.LastSeenAt.Equal(t1) {
		t.Fatalf("last_seen_at = %v, want %v", a.LastSeenAt, t1)
	}

	t2 := t1.Add(time.Hour)
	if err := r.MarkSeen(ctx, alice, t2); err != nil {
		t.Fatal(err)
	}
	a, _ = r.ActorByID(ctx, alice)
	if a.LastSeenAt == nil || !a.LastSeenAt.Equal(t2) {
		t.Fatalf("last_seen_at = %v, want %v", a.LastSeenAt, t2)
	}
}

func TestDeleteUserRevokesAccessButKeepsPastes(t *testing.T) {
	ctx := context.Background()
	r, adminID := newTestRepo(t)
	now := time.Now().UTC()

	alice, err := r.CreateUser(ctx, "alice", "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateLink(ctx, alice, "alice-token-hash", "pwhash", now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreatePaste(ctx, alice, "alice-paste", "b", "admin_only", nil, now); err != nil {
		t.Fatal(err)
	}

	if err := r.DeleteUser(ctx, alice, now); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// Account is disabled and hidden from the users list.
	a, err := r.ActorByID(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if a.IsActive {
		t.Fatal("deleted user is still active")
	}
	users, err := r.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("deleted user still listed: %v", users)
	}

	// Permanent link is revoked.
	link, err := r.LinkByTokenHash(ctx, "alice-token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if link.IsActive {
		t.Fatal("permanent link of a deleted user is still active")
	}

	// The admin keeps seeing the user's paste.
	all, err := r.ListAllPastes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Title != "alice-paste" {
		t.Fatalf("admin lost the user's paste: %v", titles(all))
	}

	// Deleting again, or deleting the admin, is a not-found.
	if err := r.DeleteUser(ctx, alice, now); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
	if err := r.DeleteUser(ctx, adminID, now); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleting the admin error = %v, want ErrNotFound", err)
	}
}

func TestActivationMakesAccountPermanent(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRepo(t)
	// Timestamps are stored with second precision, so build the fixture the
	// same way to compare exactly.
	now := time.Now().UTC().Truncate(time.Second)
	deadline := now.Add(48 * time.Hour)

	alice, err := r.CreateUser(ctx, "alice", "h", &deadline)
	if err != nil {
		t.Fatal(err)
	}

	// Before activation only the activation window applies.
	a, err := r.ActorByID(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if a.ExpiresAt == nil || !a.ExpiresAt.Equal(deadline) {
		t.Fatalf("activation deadline = %v, want %v", a.ExpiresAt, deadline)
	}
	if a.Expired(now.Add(time.Hour)) {
		t.Fatal("user must be usable inside the activation window")
	}
	if !a.Expired(now.Add(72 * time.Hour)) {
		t.Fatal("user must not be usable after the activation window")
	}

	// Activate.
	if _, err := r.Activate(ctx, alice, "token-hash", "pw-hash", now); err != nil {
		t.Fatal(err)
	}

	a, err = r.ActorByID(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if a.ExpiresAt != nil {
		t.Fatalf("activated account still has a deadline: %v", a.ExpiresAt)
	}
	if a.Expired(now.Add(100000 * time.Hour)) {
		t.Fatal("an activated account must never expire")
	}
	if a.PrimaryActive {
		t.Fatal("primary credentials must be consumed on activation")
	}
	if a.FirstSeenAt == nil {
		t.Fatal("first_seen_at must be recorded on activation")
	}

	link, err := r.LinkByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if !link.IsActive {
		t.Fatal("the permanent link must be active after activation")
	}
}

func TestActivationWindowHidesUserFromRecipients(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRepo(t)
	now := time.Now().UTC()

	past := now.Add(-time.Hour)
	future := now.Add(48 * time.Hour)

	if _, err := r.CreateUser(ctx, "windowgone", "h", &past); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateUser(ctx, "pending", "h", &future); err != nil {
		t.Fatal(err)
	}
	activated, err := r.CreateUser(ctx, "activated", "h", &future)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Activate(ctx, activated, "t", "p", now); err != nil {
		t.Fatal(err)
	}

	users, err := r.ListActiveUsers(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, u := range users {
		got[u.Username] = true
	}

	if got["windowgone"] {
		t.Error("a user whose activation window has passed must not be offered")
	}
	if !got["pending"] {
		t.Error("a user inside the activation window must be offered")
	}
	if !got["activated"] {
		t.Error("an activated (permanent) user must be offered")
	}
}
