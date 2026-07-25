// Package secrets seals settings values at rest with AES-256-GCM (Doc 2 §1.10).
// The key comes from APP_ENCRYPTION_KEY and is never stored in the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// prefix marks sealed values so plaintext rows from before encryption keep
// working: Decrypt passes anything without the prefix through unchanged, and
// the value is re-sealed on its next save. It also leaves room for a future v2.
const prefix = "enc:v1:"

type Box struct{ aead cipher.AEAD }

// New derives the AES-256 key as SHA-256(passphrase), so any non-empty
// APP_ENCRYPTION_KEY string is a valid key — no hex/base64 format pitfalls.
func New(passphrase string) (*Box, error) {
	if passphrase == "" {
		return nil, errors.New("APP_ENCRYPTION_KEY is empty")
	}
	sum := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt seals plaintext into "enc:v1:<base64(nonce || ciphertext)>".
func (b *Box) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt. Errors mean a wrong key or a
// tampered ciphertext — never silently return garbage.
func (b *Box) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, prefix) {
		return value, nil // legacy plaintext row; see prefix comment
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
