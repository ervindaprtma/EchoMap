package secrets

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	box, err := New("test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Encrypt("123456:ABC-telegram-token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, "enc:v1:") || strings.Contains(sealed, "telegram") {
		t.Fatalf("sealed value leaks or lacks prefix: %q", sealed)
	}
	plain, err := box.Decrypt(sealed)
	if err != nil || plain != "123456:ABC-telegram-token" {
		t.Fatalf("round trip failed: %q, %v", plain, err)
	}
}

func TestLegacyPlaintextPassesThrough(t *testing.T) {
	box, _ := New("k")
	got, err := box.Decrypt("plain-old-token")
	if err != nil || got != "plain-old-token" {
		t.Fatalf("legacy passthrough failed: %q, %v", got, err)
	}
}

func TestWrongKeyAndTamperFail(t *testing.T) {
	box1, _ := New("key-one")
	box2, _ := New("key-two")
	sealed, _ := box1.Encrypt("secret")

	if _, err := box2.Decrypt(sealed); err == nil {
		t.Fatal("decrypt with wrong key must fail")
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := box1.Decrypt(tampered); err == nil {
		t.Fatal("decrypt of tampered ciphertext must fail")
	}
}

func TestEmptyKeyRejected(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("empty passphrase must be rejected")
	}
}
