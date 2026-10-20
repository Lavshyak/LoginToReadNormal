package server

import (
	"net/http"
	"strings"

	"logintoreadnormal/internal/auth"
)

const genericLoginError = "Неверный логин или пароль."

// handlePrimaryForm renders the sterile primary login page.
func (s *Server) handlePrimaryForm(w http.ResponseWriter, r *http.Request) {
	csrf := s.ensureCSRF(w, r)
	s.render(w, "primary_login", &pageData{
		Title:       "Вход",
		PrimaryPath: s.cfg.PrimaryPath,
		CSRF:        csrf,
	}, http.StatusOK)
}

// handlePrimarySubmit verifies login + primary password, then issues a new
// permanent link and password exactly once.
func (s *Server) handlePrimarySubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.primaryFail(w, r, genericLoginError, http.StatusOK)
		return
	}
	if !s.limiter.allow("primary:"+clientIP(r), nowUTC()) {
		s.primaryFail(w, r, "Слишком много попыток. Повторите позже.", http.StatusTooManyRequests)
		return
	}
	if !s.checkCSRF(r) {
		s.primaryFail(w, r, genericLoginError, http.StatusOK)
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	now := nowUTC()

	actor, hash, err := s.repo.PrimaryCredential(r.Context(), username)
	if err != nil {
		auth.EqualizeTiming(password)
		s.primaryFail(w, r, genericLoginError, http.StatusOK)
		return
	}
	valid := actor.IsActive && actor.PrimaryActive && !actor.Expired(now) &&
		auth.CheckPassword(hash, password)
	if !valid {
		s.primaryFail(w, r, genericLoginError, http.StatusOK)
		return
	}

	token, err := auth.RandomToken()
	if err != nil {
		s.primaryFail(w, r, "Внутренняя ошибка. Попробуйте позже.", http.StatusInternalServerError)
		return
	}
	pw, err := auth.RandomPassword(16)
	if err != nil {
		s.primaryFail(w, r, "Внутренняя ошибка. Попробуйте позже.", http.StatusInternalServerError)
		return
	}
	pwHash, err := auth.HashPassword(pw)
	if err != nil {
		s.primaryFail(w, r, "Внутренняя ошибка. Попробуйте позже.", http.StatusInternalServerError)
		return
	}
	if _, err := s.repo.Activate(r.Context(), actor.ID, auth.HashToken(token), pwHash, now); err != nil {
		s.primaryFail(w, r, "Внутренняя ошибка. Попробуйте позже.", http.StatusInternalServerError)
		return
	}

	// Shown exactly once; no session cookie is set here by design.
	s.render(w, "credentials", &pageData{
		Title:    "Доступ выдан",
		LinkURL:  s.cfg.RootURL + base(token),
		Password: pw,
	}, http.StatusOK)
}

// primaryFail re-renders the primary form with a generic message, keeping the
// page sterile (no hints about whether the account exists).
func (s *Server) primaryFail(w http.ResponseWriter, r *http.Request, msg string, status int) {
	csrf := s.ensureCSRF(w, r)
	s.render(w, "primary_login", &pageData{
		Title:       "Вход",
		PrimaryPath: s.cfg.PrimaryPath,
		CSRF:        csrf,
		Error:       msg,
	}, status)
}
