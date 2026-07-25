// Package auth holds password hashing (argon2id) and is the crypto seam for
// Phase 8 sessions/RBAC. argon2id is the only scheme EchoMap ever stores (PRD §4.6).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP-aligned argon2id parameters. Tunable here without touching stored hashes:
// the salt and every parameter are encoded into the hash string, so Verify reads
// them back rather than assuming these constants.
const (
	argonTime    = 3         // iterations
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

var ErrMismatch = errors.New("password does not match")

// HashPassword returns a self-describing PHC-style string:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<b64salt>$<b64hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword re-derives the key with the stored parameters and compares in
// constant time. Returns ErrMismatch on a wrong password, a parse error on a
// malformed hash — callers must treat any non-nil error as "auth failed".
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=65536,t=3,p=2", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("unrecognized password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return errors.New("unsupported argon2 version")
	}
	var mem uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time, &threads); err != nil {
		return errors.New("malformed argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, time, mem, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}
