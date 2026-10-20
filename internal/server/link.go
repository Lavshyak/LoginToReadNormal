package server

import (
	"net/http"

	"logintoreadnormal/internal/auth"
	"logintoreadnormal/internal/db"
	"logintoreadnormal/internal/models"
)

// linkCtx carries the resolved link, actor and whether the request carries a
// valid session cookie bound to this very link.
type linkCtx struct {
	Token    string
	Link     *models.Link
	Actor    *models.Actor
	Authed   bool
	Baseline int64 // counter baseline (unix seconds) captured at session start
}

// resolveLink validates the token and (optionally) the session cookie. It
// returns db.ErrNotFound for unknown/inactive links or actors, so callers can
// respond with the identical generic 404.
func (s *Server) resolveLink(r *http.Request) (*linkCtx, error) {
	token := r.PathValue("token")
	link, err := s.repo.LinkByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		return nil, err
	}
	if !link.IsActive {
		return nil, db.ErrNotFound
	}
	actor, err := s.repo.ActorByID(r.Context(), link.ActorID)
	if err != nil {
		return nil, err
	}
	if !actor.IsActive {
		return nil, db.ErrNotFound
	}
	lc := &linkCtx{Token: token, Link: link, Actor: actor}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if aid, lid, base, ok := auth.VerifySession(s.cfg.SessionSecret, c.Value, nowUTC()); ok &&
			aid == actor.ID && lid == link.ID {
			lc.Authed = true
			lc.Baseline = base
		}
	}
	return lc, nil
}

// setSessionCookie issues the signed, link-bound session cookie carrying the
// counter baseline for this visit.
func (s *Server) setSessionCookie(w http.ResponseWriter, lc *linkCtx, baseline int64) {
	expires := nowUTC().Add(s.cfg.SessionTTL)
	value := auth.SignSession(s.cfg.SessionSecret, lc.Actor.ID, lc.Link.ID, expires, baseline)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     base(lc.Token),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     base(token),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// requireAuthed returns false and sends a redirect to the link root when the
// request has no valid session for this link.
func (s *Server) requireAuthed(w http.ResponseWriter, r *http.Request, lc *linkCtx) bool {
	if lc.Authed {
		return true
	}
	http.Redirect(w, r, base(lc.Token), http.StatusSeeOther)
	return false
}
