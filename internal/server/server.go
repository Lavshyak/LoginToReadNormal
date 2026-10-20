// Package server wires HTTP routing, middleware and handlers.
package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"logintoreadnormal/internal/config"
	"logintoreadnormal/internal/repo"
)

const sessionCookieName = "ltsid"

// notFoundBody is the single, identical 404 body returned for every unknown
// path. It must never hint at existing routes, links or APIs.
const notFoundBody = "<!doctype html><html lang=\"ru\"><head><meta charset=\"utf-8\">" +
	"<title>404</title></head><body><h1>404</h1><p>Not found.</p></body></html>"

// Server holds the application dependencies.
type Server struct {
	cfg     *config.Config
	repo    *repo.Repo
	tmpl    *templates
	limiter *rateLimiter
	adminID int64
}

// New builds the server, loading templates and resolving the admin id.
func New(ctx context.Context, cfg *config.Config, r *repo.Repo) (*Server, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	adminID, err := r.AdminID(ctx)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:     cfg,
		repo:    r,
		tmpl:    tmpl,
		limiter: newRateLimiter(10, time.Minute),
		adminID: adminID,
	}, nil
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Primary entry (shared, sterile).
	mux.HandleFunc("GET "+s.cfg.PrimaryPath, s.handlePrimaryForm)
	mux.HandleFunc("POST "+s.cfg.PrimaryPath, s.handlePrimarySubmit)

	// Permanent link: the interface lives under the same prefix.
	mux.HandleFunc("GET /l/{token}", s.handleLinkRoot)
	mux.HandleFunc("POST /l/{token}/unlock", s.handleUnlock)
	mux.HandleFunc("POST /l/{token}/logout", s.handleLogout)
	mux.HandleFunc("GET /l/{token}/pastes", s.handlePasteList)
	mux.HandleFunc("GET /l/{token}/pastes/new", s.handlePasteNew)
	mux.HandleFunc("POST /l/{token}/pastes", s.handlePasteCreate)
	mux.HandleFunc("GET /l/{token}/pastes/{id}", s.handlePasteView)
	mux.HandleFunc("GET /l/{token}/admin/users", s.handleAdminUsers)
	mux.HandleFunc("POST /l/{token}/admin/users", s.handleAdminCreateUser)
	mux.HandleFunc("POST /l/{token}/admin/users/{id}/delete", s.handleAdminDeleteUser)
	mux.HandleFunc("GET /l/{token}/admin/pastes/new", s.handlePasteNew)
	mux.HandleFunc("POST /l/{token}/admin/pastes", s.handlePasteCreate)
	mux.HandleFunc("POST /l/{token}/admin/pastes/{id}/delete", s.handlePasteDelete)

	// Everything else -> identical generic 404.
	mux.HandleFunc("/", s.handleNotFound)

	return s.securityHeaders(s.cleanPathGuard(mux))
}

// securityHeaders adds hardening headers to every response.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		if r.URL.Path == s.cfg.PrimaryPath {
			h.Set("X-Robots-Tag", "noindex, nofollow")
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// cleanPathGuard rejects paths that the stdlib mux would otherwise redirect
// (revealing the primary path via a 301). Such paths get the generic 404.
func (s *Server) cleanPathGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.Contains(p, "//") ||
			strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") ||
			strings.Contains(p, "/./") || strings.HasSuffix(p, "/.") {
			s.notFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) }

func (s *Server) notFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, notFoundBody)
}
