package auth

import (
	"strings"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("unexpected hash format: %q", h)
	}
	if err := VerifyPassword("correct horse battery staple", h); err != nil {
		t.Fatalf("verify should accept the right password: %v", err)
	}
	if err := VerifyPassword("wrong password", h); err == nil {
		t.Fatal("verify must reject the wrong password")
	}
}

func TestSaltIsRandom(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$bad", "$bcrypt$..."} {
		if err := VerifyPassword("x", bad); err == nil {
			t.Fatalf("verify must error on malformed hash %q", bad)
		}
	}
}
