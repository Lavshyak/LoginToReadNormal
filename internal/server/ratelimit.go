package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a tiny in-memory fixed-window counter, enough to slow down
// password guessing without any external dependency.
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rlEntry
	limit   int
	window  time.Duration
}

type rlEntry struct {
	count int
	start time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		entries: make(map[string]*rlEntry),
		limit:   limit,
		window:  window,
	}
}

// allow reports whether the key may proceed, incrementing its counter.
func (rl *rateLimiter) allow(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Opportunistic cleanup of expired windows.
	for k, e := range rl.entries {
		if now.Sub(e.start) > rl.window {
			delete(rl.entries, k)
		}
	}

	e, ok := rl.entries[key]
	if !ok || now.Sub(e.start) > rl.window {
		rl.entries[key] = &rlEntry{count: 1, start: now}
		return true
	}
	e.count++
	return e.count <= rl.limit
}

// clientIP extracts the best-guess client address (nginx sets X-Real-IP).
func clientIP(r *http.Request) string {
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
