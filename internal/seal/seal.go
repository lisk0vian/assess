// Package seal encrypts single database values with AES-256-GCM, so an
// encrypted vault's assess.db can be an ordinary SQLite file whose sensitive
// columns (exam content, names, identifiers, answers) are unreadable.
//
// Sealed values carry a prefix, so a column may hold plain and sealed values
// side by side while a vault is being encrypted or decrypted. A nil *Cipher
// leaves values as they are.
package seal

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// KeySize is the length of a master key.
const KeySize = 32

// ErrLocked is returned when a sealed value is read, or a value would be
// written, without the vault's key.
var ErrLocked = errors.New("the vault is locked")

// ErrCorrupt is returned for a sealed value that does not open with the key.
var ErrCorrupt = errors.New("a sealed value does not open with this vault's key")

// Prefixes of sealed values. Blobs start with a byte JSON never starts with.
var blobPrefix = []byte{0xff, 'A', 'X', '1'}

const (
	textPrefix = "ax1:"  // random nonce
	detPrefix  = "axd1:" // deterministic: equal values seal equally
)

// Cipher seals and opens values with keys derived from one master key.
type Cipher struct {
	random cipher.AEAD
	det    cipher.AEAD
	detMAC []byte
}

// New derives the cipher of a master key.
func New(master []byte) (*Cipher, error) {
	if len(master) != KeySize {
		return nil, errors.New("seal: the key must have 32 bytes")
	}
	derive := func(info string) ([]byte, error) { return hkdf.Key(sha256.New, master, nil, "assess/seal/"+info, 32) }
	k1, err := derive("random")
	if err != nil {
		return nil, err
	}
	k2, err := derive("det")
	if err != nil {
		return nil, err
	}
	mac, err := derive("det-nonce")
	if err != nil {
		return nil, err
	}
	c := &Cipher{detMAC: mac}
	if c.random, err = gcm(k1); err != nil {
		return nil, err
	}
	if c.det, err = gcm(k2); err != nil {
		return nil, err
	}
	return c, nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// IsSealed reports whether b is a sealed blob.
func IsSealed(b []byte) bool { return bytes.HasPrefix(b, blobPrefix) }

// IsSealedText reports whether s is a sealed text.
func IsSealedText(s string) bool {
	return strings.HasPrefix(s, textPrefix) || strings.HasPrefix(s, detPrefix)
}

func (c *Cipher) seal(aead cipher.AEAD, nonce, plain []byte) []byte {
	return aead.Seal(nonce, nonce, plain, nil)
}

func (c *Cipher) open(aead cipher.AEAD, b []byte) ([]byte, error) {
	n := aead.NonceSize()
	if len(b) < n {
		return nil, ErrCorrupt
	}
	out, err := aead.Open(nil, b[:n], b[n:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return out, nil
}

func (c *Cipher) randomNonce() []byte {
	n := make([]byte, c.random.NonceSize())
	if _, err := rand.Read(n); err != nil {
		panic(err)
	}
	return n
}

func (c *Cipher) detNonce(plain []byte) []byte {
	m := hmac.New(sha256.New, c.detMAC)
	m.Write(plain)
	return m.Sum(nil)[:c.det.NonceSize()]
}

// Blob seals b; a nil cipher returns it as it is.
func (c *Cipher) Blob(b []byte) []byte {
	if c == nil || b == nil {
		return b
	}
	return append(append([]byte{}, blobPrefix...), c.seal(c.random, c.randomNonce(), b)...)
}

// OpenBlob returns the plain value of b, sealed or not.
func (c *Cipher) OpenBlob(b []byte) ([]byte, error) {
	if !IsSealed(b) {
		return b, nil
	}
	if c == nil {
		return nil, ErrLocked
	}
	return c.open(c.random, b[len(blobPrefix):])
}

var b64 = base64.RawURLEncoding

// Text seals s; the empty text stays empty.
func (c *Cipher) Text(s string) string {
	if c == nil || s == "" {
		return s
	}
	return textPrefix + b64.EncodeToString(c.seal(c.random, c.randomNonce(), []byte(s)))
}

// Det seals s so that the same text always gives the same result: the
// column can still be compared, indexed and joined on.
func (c *Cipher) Det(s string) string {
	if c == nil || s == "" {
		return s
	}
	return detPrefix + b64.EncodeToString(c.seal(c.det, c.detNonce([]byte(s)), []byte(s)))
}

// OpenText returns the plain value of s, sealed or not.
func (c *Cipher) OpenText(s string) (string, error) {
	var aead cipher.AEAD
	var body string
	switch {
	case strings.HasPrefix(s, textPrefix):
		body = s[len(textPrefix):]
		if c != nil {
			aead = c.random
		}
	case strings.HasPrefix(s, detPrefix):
		body = s[len(detPrefix):]
		if c != nil {
			aead = c.det
		}
	default:
		return s, nil
	}
	if c == nil {
		return "", ErrLocked
	}
	raw, err := b64.DecodeString(body)
	if err != nil {
		return "", ErrCorrupt
	}
	out, err := c.open(aead, raw)
	return string(out), err
}
