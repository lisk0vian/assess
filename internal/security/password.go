// Package security holds the teacher's panel password. It protects the TUI,
// not the disk: anyone using the teacher's OS account can read the database.
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (RFC 9106 second recommendation, lighter memory).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
	// MinLength keeps passwords from being trivially guessable at the keyboard.
	MinLength = 6
)

// ErrTooShort rejects weak passwords.
var ErrTooShort = fmt.Errorf("the password needs at least %d characters", MinLength)

type adminFile struct {
	PasswordHash string `json:"password_hash"`
}

func path(dir string) string { return filepath.Join(dir, "admin.json") }

// SetPassword stores an Argon2id hash of pw for the data directory.
func SetPassword(dir, pw string) error {
	if len([]rune(pw)) < MinLength {
		return ErrTooShort
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding.EncodeToString
	hash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, enc(salt), enc(key))
	data, _ := json.MarshalIndent(adminFile{PasswordHash: hash}, "", "  ")
	return os.WriteFile(path(dir), data, 0o600)
}

// ClearPassword removes the password.
func ClearPassword(dir string) error {
	err := os.Remove(path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// HasPassword reports whether the panel is protected.
func HasPassword(dir string) bool {
	h, err := load(dir)
	return err == nil && h != ""
}

func load(dir string) (string, error) {
	data, err := os.ReadFile(path(dir))
	if err != nil {
		return "", err
	}
	var f adminFile
	if err := json.Unmarshal(data, &f); err != nil {
		return "", err
	}
	return f.PasswordHash, nil
}

// CheckPassword verifies pw. Without a stored password every input passes.
func CheckPassword(dir, pw string) bool {
	stored, err := load(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	return verify(stored, pw)
}

func verify(stored, pw string) bool {
	parts := strings.Split(stored, "$") // "", argon2id, v=19, m=..,t=..,p=.., salt, key
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want))) //nolint:gosec // G115: key length is 32
	return subtle.ConstantTimeCompare(got, want) == 1
}
