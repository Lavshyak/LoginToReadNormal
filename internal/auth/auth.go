// Package auth provides cryptographic helpers: token/password generation,
// hashing and the signed session cookie used to bind a session to a link.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is a balance between safety and low memory/CPU on a small box.
const bcryptCost = 11

const passwordAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var tokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// RandomToken returns a URL-safe random secret (32 random bytes -> base32).
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(tokenEncoding.EncodeToString(b)), nil
}

// RandomPassword returns a random password of length n from an unambiguous
// alphabet (no 0/O/1/l/I).
func RandomPassword(n int) (string, error) {
	if n <= 0 {
		n = 16
	}
	out := make([]byte, n)
	max := byte(len(passwordAlphabet))
	for i := range out {
		// Rejection sampling keeps the distribution uniform.
		for {
			b := make([]byte, 1)
			if _, err := rand.Read(b); err != nil {
				return "", err
			}
			if int(b[0]) < 256-(256%int(max)) {
				out[i] = passwordAlphabet[int(b[0])%int(max)]
				break
			}
		}
	}
	return string(out), nil
}

// HashToken returns the hex SHA-256 of a raw token; only this hash is stored.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// HashPassword bcrypt-hashes a password.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword verifies a password against a bcrypt hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// DummyBcryptHash is a valid bcrypt hash used to equalise timing when a login
// does not exist, so failures cannot be distinguished by response time.
var DummyBcryptHash = func() string {
	h, err := bcrypt.GenerateFromPassword([]byte("invalid"), bcryptCost)
	if err != nil {
		return ""
	}
	return string(h)
}()

// EqualizeTiming performs a dummy bcrypt comparison.
func EqualizeTiming(pw string) {
	_ = bcrypt.CompareHashAndPassword([]byte(DummyBcryptHash), []byte(pw))
}

// ConstantTimeEqual compares two strings without leaking length-timing beyond
// what subtle.ConstantTimeCompare already tolerates.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// NewCSRF returns a random CSRF token.
func NewCSRF() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// --- Session cookie -------------------------------------------------------
//
// Format: base64url(payload) "." base64url(HMAC-SHA256(payload, secret))
// payload: actor_id ":" link_id ":" expires_unix ":" baseline_unix
//
// baseline_unix is the "new pastes" counter baseline captured when the session
// started (the previous visit), so the counter stays stable during the session.

// SignSession builds a signed cookie value bound to (actorID, linkID).
func SignSession(secret []byte, actorID, linkID int64, expires time.Time, baseline int64) string {
	payload := fmt.Sprintf("%d:%d:%d:%d", actorID, linkID, expires.Unix(), baseline)
	return payload + "." + sign(secret, payload)
}

// VerifySession parses and validates a cookie value. It returns the bound
// actorID, linkID and counter baseline when the signature is valid and the
// session has not expired.
func VerifySession(secret []byte, value string, now time.Time) (actorID, linkID, baseline int64, ok bool) {
	payload, sig, found := strings.Cut(value, ".")
	if !found {
		return 0, 0, 0, false
	}
	expected := sign(secret, payload)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return 0, 0, 0, false
	}
	parts := strings.Split(payload, ":")
	if len(parts) != 4 {
		return 0, 0, 0, false
	}
	a, err1 := strconv.ParseInt(parts[0], 10, 64)
	l, err2 := strconv.ParseInt(parts[1], 10, 64)
	exp, err3 := strconv.ParseInt(parts[2], 10, 64)
	base, err4 := strconv.ParseInt(parts[3], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return 0, 0, 0, false
	}
	if now.After(time.Unix(exp, 0)) {
		return 0, 0, 0, false
	}
	return a, l, base, true
}

func sign(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
