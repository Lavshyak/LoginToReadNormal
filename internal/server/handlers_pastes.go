package server

import (
	"net/http"
	"strconv"
	"strings"

	"logintoreadnormal/internal/db"
)

// handlePasteNew renders the paste creation form. Admins additionally choose
// an audience; users always create admin-only pastes.
func (s *Server) handlePasteNew(w http.ResponseWriter, r *http.Request) {
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
	d := s.appData(w, r, lc)
	d.Title = "Новая паста"
	if lc.Actor.IsAdmin() {
		if users, err := s.repo.ListActiveUsers(r.Context(), nowUTC()); err == nil {
			d.Users = users
		}
	}
	s.render(w, "paste_new", d, http.StatusOK)
}

// handlePasteCreate stores a paste (admin: with audience; user: admin_only).
func (s *Server) handlePasteCreate(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		s.renderError(w, s.appData(w, r, lc), "Некорректная форма.", http.StatusBadRequest)
		return
	}
	if !s.checkCSRF(r) {
		s.renderError(w, s.appData(w, r, lc), "Неверный CSRF-токен. Обновите страницу.", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.PostFormValue("title"))
	body := r.PostFormValue("body")
	if title == "" {
		s.renderError(w, s.appData(w, r, lc), "Укажите заголовок пасты.", http.StatusBadRequest)
		return
	}

	audience := "admin_only"
	var recipients []int64
	if lc.Actor.IsAdmin() {
		audience = r.PostFormValue("audience")
		if audience != "all" && audience != "specific" {
			audience = "all"
		}
		if audience == "specific" {
			recipients = parseRecipients(r)
		}
	}

	if _, err := s.repo.CreatePaste(r.Context(), lc.Actor.ID, title, body, audience, recipients, nowUTC()); err != nil {
		s.renderError(w, s.appData(w, r, lc), "Не удалось сохранить пасту.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, base(lc.Token), http.StatusSeeOther)
}

// handlePasteView shows a paste the actor is allowed to see.
func (s *Server) handlePasteView(w http.ResponseWriter, r *http.Request) {
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

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	p, err := s.repo.PasteByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	can, err := s.repo.CanView(r.Context(), lc.Actor, s.adminID, p)
	if err != nil || !can {
		s.notFound(w, r)
		return
	}

	v, err := s.pasteViewFor(r.Context(), lc.Actor, *p)
	if err != nil {
		s.renderError(w, s.appData(w, r, lc), "Не удалось загрузить получателей пасты.", http.StatusInternalServerError)
		return
	}

	d := s.appData(w, r, lc)
	d.Title = p.Title
	d.Paste = &v
	s.render(w, "paste_view", d, http.StatusOK)
}

// handlePasteDelete soft-deletes a paste (admin only).
func (s *Server) handlePasteDelete(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.repo.PasteByID(r.Context(), id); err != nil {
		if err == db.ErrNotFound {
			s.notFound(w, r)
			return
		}
		s.renderError(w, s.appData(w, r, lc), "Не удалось удалить пасту.", http.StatusInternalServerError)
		return
	}
	if err := s.repo.SoftDeletePaste(r.Context(), id, nowUTC()); err != nil {
		s.renderError(w, s.appData(w, r, lc), "Не удалось удалить пасту.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, base(lc.Token), http.StatusSeeOther)
}

// parseRecipients extracts and de-duplicates the "recipients" checkbox values.
func parseRecipients(r *http.Request) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, raw := range r.Form["recipients"] {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
