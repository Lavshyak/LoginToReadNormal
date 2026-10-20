package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the whole runtime configuration, sourced from the environment
// (optionally pre-populated from a .env file).
type Config struct {
	RootURL          string // ROOT_URL, e.g. https://paste.example.com
	PrimaryPath      string // PRIMARY_URL_PATH, e.g. /login (shared primary entry)
	AdminLogin       string // ADMIN_LOGIN
	AdminPrimaryPass string // ADMIN_PRIMARY_PASSWORD (only used for the first seed)
	SessionSecret    []byte // SESSION_SECRET (HMAC key for cookies)
	DBPath           string // DB_PATH
	ListenAddr       string // LISTEN_ADDR
	SessionTTL       time.Duration
	DefaultUserTTL   time.Duration
	CookieSecure     bool // COOKIE_SECURE (default true; disable only for plain-http testing)
}

const (
	defaultDBPath     = "/data/app.db"
	defaultListenAddr = ":8080"
	defaultSessionTTL = 12 * time.Hour
	defaultUserTTL    = 30 * 24 * time.Hour
)

// Load builds the configuration from the environment, applying defaults.
func Load() (*Config, error) {
	if err := LoadDotEnv(); err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}

	cfg := &Config{
		RootURL:          strings.TrimRight(env("ROOT_URL", "http://localhost"), "/"),
		PrimaryPath:      env("PRIMARY_URL_PATH", "/login"),
		AdminLogin:       env("ADMIN_LOGIN", "admin"),
		AdminPrimaryPass: os.Getenv("ADMIN_PRIMARY_PASSWORD"),
		SessionSecret:    []byte(os.Getenv("SESSION_SECRET")),
		DBPath:           env("DB_PATH", defaultDBPath),
		ListenAddr:       env("LISTEN_ADDR", defaultListenAddr),
	}

	primary, err := normalizePrimaryPath(cfg.PrimaryPath)
	if err != nil {
		return nil, err
	}
	cfg.PrimaryPath = primary

	if cfg.AdminLogin == "" {
		return nil, fmt.Errorf("ADMIN_LOGIN must not be empty")
	}
	if cfg.AdminPrimaryPass == "" {
		return nil, fmt.Errorf("ADMIN_PRIMARY_PASSWORD must not be empty")
	}
	if len(cfg.SessionSecret) < 16 {
		return nil, fmt.Errorf("SESSION_SECRET must be set and at least 16 bytes long")
	}

	cfg.SessionTTL = envDuration("SESSION_TTL", defaultSessionTTL)
	cfg.DefaultUserTTL = envDuration("DEFAULT_USER_TTL", defaultUserTTL)
	cfg.CookieSecure = envBool("COOKIE_SECURE", true)

	return cfg, nil
}

// normalizePrimaryPath validates and cleans PRIMARY_URL_PATH. The primary entry
// must be a single, non-root path segment (optionally a nested prefix) that does
// not collide with the reserved "/l/" interface prefix.
func normalizePrimaryPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("PRIMARY_URL_PATH must not be empty")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	if p == "" || p == "/" {
		return "", fmt.Errorf("PRIMARY_URL_PATH must not be the root path \"/\"")
	}
	if p == "/l" || strings.HasPrefix(p, "/l/") {
		return "", fmt.Errorf("PRIMARY_URL_PATH must not collide with the reserved /l/ prefix")
	}
	if strings.ContainsAny(p, " \t?#") {
		return "", fmt.Errorf("PRIMARY_URL_PATH must not contain spaces, '?' or '#'")
	}
	return p, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	// Accept either a Go duration ("12h", "720h") or plain seconds.
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	return def
}
