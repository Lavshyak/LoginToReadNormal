package server

import (
	"net/http"
	"time"

	"logintoreadnormal/internal/auth"
	"logintoreadnormal/internal/models"
)

// handleLinkRoot shows the interface when the cookie is valid for this link,
// otherwise the permanent-password form (same URL).
func (s *Server) handleLinkRoot(w http.ResponseWriter, r *http.Request) {
	lc, err := s.resolveLink(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if lc.Actor.Expired(nowUTC()) {
		s.render(w, "expired", &pageData{Title: "Доступ истёк"}, http.StatusForbidden)
		return
	}
	if lc.Authed {
		s.renderDashboard(w, r, lc, "")
		return
	}
	csrf := s.ensureCSRF(w, r)
	s.render(w, "link_unlock", &pageData{
		Title: "Вход",
		Base:  base(lc.Token),
		CSRF:  csrf,
	}, http.StatusOK)
}

// handleUnlock verifies the permanent password bound to this link and, on
// success, sets the session cookie and returns to the same link.
func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	lc, err := s.resolveLink(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if lc.Actor.Expired(nowUTC()) {
		s.render(w, "expired", &pageData{Title: "Доступ истёк"}, http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.unlockFail(w, r, lc, genericLoginError)
		return
	}
	if !s.limiter.allow("unlock:"+clientIP(r)+":"+lc.Token, nowUTC()) {
		s.unlockFail(w, r, lc, "Слишком много попыток. Повторите позже.")
		return
	}
	if !s.checkCSRF(r) {
		s.unlockFail(w, r, lc, genericLoginError)
		return
	}
	if !auth.CheckPassword(lc.Link.PasswordHash, r.PostFormValue("password")) {
		s.unlockFail(w, r, lc, genericLoginError)
		return
	}

	// The counter of the upcoming visit counts from the moment of the previous
	// visit; remember it in the cookie, then move the marker to now.
	var baseline int64
	if b := lc.Actor.CounterBaseline(); b != nil {
		baseline = b.Unix()
	}
	if err := s.repo.MarkSeen(r.Context(), lc.Actor.ID, nowUTC()); err != nil {
		s.renderError(w, s.appData(w, r, lc), "Внутренняя ошибка.", http.StatusInternalServerError)
		return
	}

	s.setSessionCookie(w, lc, baseline)
	http.Redirect(w, r, base(lc.Token), http.StatusSeeOther)
}

func (s *Server) unlockFail(w http.ResponseWriter, r *http.Request, lc *linkCtx, msg string) {
	csrf := s.ensureCSRF(w, r)
	s.render(w, "link_unlock", &pageData{
		Title: "Вход",
		Base:  base(lc.Token),
		CSRF:  csrf,
		Error: msg,
	}, http.StatusOK)
}

// handleLogout clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	s.clearSessionCookie(w, token)
	http.Redirect(w, r, base(token), http.StatusSeeOther)
}

// handlePasteList renders the same dashboard (the list lives there).
func (s *Server) handlePasteList(w http.ResponseWriter, r *http.Request) {
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
	s.renderDashboard(w, r, lc, "")
}

// renderDashboard lists the pastes visible to the actor, newest first and
// expanded, plus the "new since the previous visit" counter for regular users.
func (s *Server) renderDashboard(w http.ResponseWriter, r *http.Request, lc *linkCtx, notice string) {
	ctx := r.Context()
	var (
		pastes []models.Paste
		err    error
	)
	if lc.Actor.IsAdmin() {
		pastes, err = s.repo.ListAllPastes(ctx)
	} else {
		pastes, err = s.repo.ListPastesForUser(ctx, lc.Actor.ID, s.adminID)
	}
	if err != nil {
		s.renderError(w, s.appData(w, r, lc), "Не удалось загрузить пасты.", http.StatusInternalServerError)
		return
	}

	views := make([]pasteView, 0, len(pastes))
	for _, p := range pastes {
		views = append(views, newPasteView(p, lc.Actor.IsAdmin()))
	}

	d := s.appData(w, r, lc)
	d.Title = "Пасты"
	d.Pastes = views
	d.Notice = notice
	if !lc.Actor.IsAdmin() {
		d.ShowCounter = true
		if lc.Baseline > 0 {
			since := time.Unix(lc.Baseline, 0).UTC()
			if n, err := s.repo.CountNewPastesForUser(ctx, lc.Actor.ID, s.adminID, since); err == nil {
				d.Counter = n
			}
		}
	}
	s.render(w, "dashboard", d, http.StatusOK)
}

// appData builds the common page payload for authenticated app pages.
func (s *Server) appData(w http.ResponseWriter, r *http.Request, lc *linkCtx) *pageData {
	return &pageData{
		Base:    base(lc.Token),
		Actor:   lc.Actor,
		IsAdmin: lc.Actor.IsAdmin(),
		CSRF:    s.ensureCSRF(w, r),
	}
}
