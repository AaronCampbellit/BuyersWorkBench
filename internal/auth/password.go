// Package auth contains the local authentication primitives shared by CLI/API.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory    uint32 = 64 * 1024 // KiB: above the 19 MiB minimum.
	passwordTime      uint32 = 3
	passwordThreads   uint8  = 1
	passwordKeyLength uint32 = 32
)

var ErrInvalidHash = errors.New("invalid password hash")

// HashPassword uses the maintained x/crypto Argon2id implementation. Encodings
// include a unique random salt and parameters so later upgrades are possible.
func HashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 14 || len(password) > 1024 {
		return "", errors.New("password must contain at least 14 characters and at most 1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, passwordTime, passwordMemory, passwordThreads, passwordKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, passwordMemory, passwordTime, passwordThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword rejects malformed/out-of-budget hashes before running Argon2.
// Bounds prevent a corrupted database value requesting unbounded memory or CPU.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || len(password) > 1024 {
		return false, ErrInvalidHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false, ErrInvalidHash
	}
	values := make([]uint64, 3)
	for i, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(params[i], prefix) {
			return false, ErrInvalidHash
		}
		var err error
		values[i], err = strconv.ParseUint(strings.TrimPrefix(params[i], prefix), 10, 32)
		if err != nil {
			return false, ErrInvalidHash
		}
	}
	if values[0] < 19*1024 || values[0] > 256*1024 || values[1] < 2 || values[1] > 10 || values[2] < 1 || values[2] > 4 {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false, ErrInvalidHash
	}
	wanted, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(wanted) < 32 || len(wanted) > 64 {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, uint32(values[1]), uint32(values[0]), uint8(values[2]), uint32(len(wanted)))
	return subtle.ConstantTimeCompare(got, wanted) == 1, nil
}
