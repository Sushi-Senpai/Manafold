// Package passwordhash owns credential hashing for account-access. It hashes
// with argon2id and encodes the parameters into the hash string so they can be
// raised later without a schema change; Verify also accepts a bcrypt hash so
// bcrypt stays a drop-in fallback (see
// docs/intent/account-access/account-access-design.md § Password hashing).
//
// @spec ACCT-010, ACCT-011, ACCT-018
package passwordhash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// argon2id parameters: the OWASP second-choice profile (m = 19 MiB, t = 2,
// p = 1), chosen low on memory so concurrent logins stay within the Render free
// tier's container ceiling. Stored in every encoded hash, so a future raise
// applies to new hashes while old ones keep verifying against their own params.
const (
	argonMemoryKiB = 19456
	argonTime      = 2
	argonParallel  = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// ErrMalformedHash is returned by Verify when the encoded hash cannot be parsed.
// A wrong password is not an error — Verify returns (false, nil) for that.
var ErrMalformedHash = errors.New("passwordhash: malformed encoded hash")

// Hash returns an argon2id encoded hash of password:
// $argon2id$v=19$m=19456,t=2,p=1$<b64 salt>$<b64 key>.
func Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("passwordhash: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonParallel, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemoryKiB, argonTime, argonParallel,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether password matches encoded. It dispatches on the encoded
// prefix: an argon2id string is checked against its own embedded parameters; a
// bcrypt string ($2a$/$2b$/$2y$) is checked with the bcrypt package. A wrong
// password yields (false, nil); only an unparseable hash yields an error.
func Verify(encoded, password string) (bool, error) {
	switch {
	case strings.HasPrefix(encoded, "$argon2id$"):
		return verifyArgon2id(encoded, password)
	case strings.HasPrefix(encoded, "$2a$"), strings.HasPrefix(encoded, "$2b$"), strings.HasPrefix(encoded, "$2y$"):
		err := bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password))
		if err == nil {
			return true, nil
		}
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		return false, fmt.Errorf("%w: %v", ErrMalformedHash, err)
	default:
		return false, ErrMalformedHash
	}
}

func verifyArgon2id(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=19456,t=2,p=1", "<salt>", "<key>"]
	if len(parts) != 6 {
		return false, ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrMalformedHash
	}

	var memory uint32
	var time uint32
	var parallel uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &parallel); err != nil {
		return false, ErrMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrMalformedHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrMalformedHash
	}

	got := argon2.IDKey([]byte(password), salt, time, memory, parallel, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
