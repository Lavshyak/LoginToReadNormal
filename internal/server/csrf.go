package server

import (
	"net/http"

	"logintoreadnormal/internal/auth"
)

const csrfCookieName = "ltcsrf"

// ensureCSRF returns the current CSRF token, issuing a cookie if needed.
func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	tok, err := auth.NewCSRF()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   3600,
	})
	return tok
}

// checkCSRF validates the double-submit token (form value vs cookie).
func (s *Server) checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	form := r.PostFormValue("csrf")
	return form != "" && auth.ConstantTimeEqual(form, c.Value)
}
