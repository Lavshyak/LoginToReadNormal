package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"logintoreadnormal/internal/auth"
	"logintoreadnormal/internal/db"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)

// handleAdminUsers lists users and offers the creation form (admin only).
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	lc, err := s.resolveLink(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if lc.Actor.Expired(nowUTC()) {
		s.render(w, "expired", &pageData{Title: "Доступ истёк"}, http.StatusForbidden)
		return
	}
	if !s.requireAuthed(w, r, lc) {
		return
	}
	if !lc.Actor.IsAdmin() {
		s.notFound(w, r)
		return
	}
	notice := ""
	if r.URL.Query().Get("deleted") == "1" {
		notice = "Пользователь удалён. Его доступ и постоянная ссылка отозваны."
	}
	s.renderUsers(w, r, lc, notice)
}

// handleAdminCreateUser creates a user with a TTL and returns a one-time
// primary password (login = username).
func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	lc, err := s.resolveLink(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if lc.Actor.Expired(nowUTC()) {
		s.render(w, "expired", &pageData{Title: "Доступ истёк"}, http.StatusForbidden)
		return
	}
	if !s.requireAuthed(w, r, lc) {
		return
	}
	if !lc.Actor.IsAdmin() {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil || !s.checkCSRF(r) {
		s.renderUsers(w, r, lc, "Неверный CSRF-токен. Обновите страницу.")
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	if !usernameRe.MatchString(username) {
		s.renderUsers(w, r, lc, "Недопустимый юзернейм: 3–64 символа из A–Z a–z 0–9 . _ -")
		return
	}
	exists, err := s.repo.UsernameExists(r.Context(), username)
	if err != nil {
		s.renderUsers(w, r, lc, "Внутренняя ошибка.")
		return
	}
	if exists {
		s.renderUsers(w, r, lc, "Такой юзернейм уже занят.")
		return
	}

	now := nowUTC()
	days := 0
	if v := strings.TrimSpace(r.PostFormValue("ttl_days")); v != "" {
		days, _ = strconv.Atoi(v)
	}
	var expires *time.Time
	if days > 0 {
		e := now.Add(time.Duration(days) * 24 * time.Hour)
		expires = &e
	} else if s.cfg.DefaultUserTTL > 0 {
		e := now.Add(s.cfg.DefaultUserTTL)
		expires = &e
	}

	primaryPassword, err := auth.RandomPassword(16)
	if err != nil {
		s.renderUsers(w, r, lc, "Внутренняя ошибка.")
		return
	}
	hash, err := auth.HashPassword(primaryPassword)
	if err != nil {
		s.renderUsers(w, r, lc, "Внутренняя ошибка.")
		return
	}
	if _, err := s.repo.CreateUser(r.Context(), username, hash, expires); err != nil {
		s.renderUsers(w, r, lc, "Не удалось создать пользователя.")
		return
	}

	notice := fmt.Sprintf(
		"Пользователь %q создан. Логин: %s. Первичный пароль (показывается один раз): %s",
		username, username, primaryPassword)
	s.renderUsers(w, r, lc, notice)
}

// handleAdminDeleteUser removes (deactivates) a user and revokes their link.
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	lc, err := s.resolveLink(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if lc.Actor.Expired(nowUTC()) {
		s.render(w, "expired", &pageData{Title: "Доступ истёк"}, http.StatusForbidden)
		return
	}
	if !s.requireAuthed(w, r, lc) {
		return
	}
	if !lc.Actor.IsAdmin() {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil || !s.checkCSRF(r) {
		s.renderError(w, s.appData(w, r, lc), "Неверный CSRF-токен. Обновите страницу.", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.repo.DeleteUser(r.Context(), id, nowUTC()); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.renderError(w, s.appData(w, r, lc), "Не удалось удалить пользователя.", http.StatusInternalServerError)
		return
	}
	// Redirect (POST/Redirect/GET) so a page refresh does not repeat the delete.
	http.Redirect(w, r, base(lc.Token)+"/admin/users?deleted=1", http.StatusSeeOther)
}

// renderUsers renders the admin users page.
func (s *Server) renderUsers(w http.ResponseWriter, r *http.Request, lc *linkCtx, notice string) {
	users, err := s.repo.ListUsers(r.Context())
	if err != nil {
		s.renderError(w, s.appData(w, r, lc), "Не удалось загрузить пользователей.", http.StatusInternalServerError)
		return
	}
	d := s.appData(w, r, lc)
	d.Title = "Пользователи"
	d.Users = users
	d.Notice = notice
	d.PrimaryPath = s.cfg.PrimaryPath
	s.render(w, "users", d, http.StatusOK)
}
