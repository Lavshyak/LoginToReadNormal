package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifySession(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Now().Add(time.Hour)

	val := SignSession(secret, 7, 42, exp, 1700000000)
	aid, lid, base, ok := VerifySession(secret, val, time.Now())
	if !ok {
		t.Fatalf("expected valid session")
	}
	if aid != 7 || lid != 42 {
		t.Fatalf("got actor=%d link=%d, want 7/42", aid, lid)
	}
	if base != 1700000000 {
		t.Fatalf("got baseline=%d, want 1700000000", base)
	}
}

func TestVerifySessionRejectsTampering(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	val := SignSession(secret, 1, 2, time.Now().Add(time.Hour), 100)

	// Wrong secret.
	if _, _, _, ok := VerifySession([]byte("another-secret-value-1234567890"), val, time.Now()); ok {
		t.Fatal("accepted a cookie signed with a different secret")
	}
	// Tampered payload.
	tampered := "9:2:9999999999:100." + strings.SplitN(val, ".", 2)[1]
	if _, _, _, ok := VerifySession(secret, tampered, time.Now()); ok {
		t.Fatal("accepted a tampered payload")
	}
	// Expired.
	expired := SignSession(secret, 1, 2, time.Now().Add(-time.Minute), 100)
	if _, _, _, ok := VerifySession(secret, expired, time.Now()); ok {
		t.Fatal("accepted an expired session")
	}
	// Garbage.
	if _, _, _, ok := VerifySession(secret, "garbage", time.Now()); ok {
		t.Fatal("accepted garbage")
	}
	// Old 3-field payload format is no longer accepted.
	if _, _, _, ok := VerifySession(secret, "1:2:9999999999."+strings.SplitN(val, ".", 2)[1], time.Now()); ok {
		t.Fatal("accepted a legacy payload without baseline")
	}
}

func TestRandomTokenAndPassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := RandomToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) != 52 { // 32 bytes base32 without padding
			t.Fatalf("unexpected token length %d", len(tok))
		}
		if seen[tok] {
			t.Fatal("duplicate token generated")
		}
		seen[tok] = true
	}

	pw, err := RandomPassword(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 16 {
		t.Fatalf("unexpected password length %d", len(pw))
	}
	for _, r := range pw {
		if !strings.ContainsRune(passwordAlphabet, r) {
			t.Fatalf("password contains unexpected rune %q", r)
		}
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "s3cret") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestHashTokenIsStable(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Fatal("hash is not deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatal("different inputs produced the same hash")
	}
	if len(HashToken("abc")) != 64 { // hex sha-256
		t.Fatal("unexpected hash length")
	}
}
