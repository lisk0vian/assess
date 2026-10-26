// Package vault keeps a teacher's exams, classes, session archives,
// backups and database in one folder that can be copied to another
// computer. A vault is encrypted unless the teacher chose otherwise.
//
// Layout:
//
//	vault.yaml               name, format and, when encrypted, the wrapped key
//	assess.db                SQLite; sensitive columns sealed (package seal)
//	exams/<id>.examx         exam packages
//	classes/<name>.yaml      a readable copy of each class list
//	sessions/YYYY/<date> <exam> <class>/{summary.md,results.csv,record.json}
//	backups/                 automatic and manual backups
//
// In an encrypted vault every file but vault.yaml and assess.db ends in .age
// and is encrypted to the vault's age recipient, so files can be added
// without the password; reading them needs the identity, which vault.yaml
// keeps wrapped twice: with the password (scrypt) and with a recovery key
// shown once when the vault is created.
//
// The vault protects against someone reading the disk: a shared or stolen
// computer, a copied folder or a lost USB stick. It does not protect
// against malware running in the teacher's open session.
package vault

import (
	"bytes"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
	"go.yaml.in/yaml/v3"

	"github.com/lisk0vian/assess/internal/seal"
)

// File and folder names.
const (
	MetaFile   = "vault.yaml"
	DBFile     = "assess.db"
	LocalDir   = ".assess-vault" // a vault found by walking up from the current folder
	Ext        = ".age"
	ExamsDir   = "exams"
	ClassesDir = "classes"
	SessionDir = "sessions"
	BackupDir  = "backups"
	format     = 1
)

// ErrNotVault is returned when a folder holds no vault.
var ErrNotVault = errors.New("not a vault (no vault.yaml)")

// ErrWrongPassword is returned when a password or recovery key does not
// unwrap the vault's key.
var ErrWrongPassword = errors.New("wrong password")

// ErrLocked is returned when an encrypted file is read without the key.
var ErrLocked = seal.ErrLocked

// MinPassword keeps passwords from being trivially guessable.
const MinPassword = 8

// ErrShortPassword rejects weak passwords.
var ErrShortPassword = fmt.Errorf("the password needs at least %d characters", MinPassword)

// ScryptWorkFactor is the cost of deriving the key from the password
// (2^n); tests lower it.
var ScryptWorkFactor = 18

// Meta is vault.yaml.
type Meta struct {
	Format      int       `yaml:"format"`
	ID          string    `yaml:"id"`
	Name        string    `yaml:"name"`
	Created     time.Time `yaml:"created"`
	Encrypted   bool      `yaml:"encrypted"`
	Recipient   string    `yaml:"recipient,omitempty"`
	PasswordKey string    `yaml:"password_key,omitempty"`
	RecoveryKey string    `yaml:"recovery_key,omitempty"`
}

// Vault is an opened vault folder.
type Vault struct {
	Dir  string
	Meta Meta
	id   *age.X25519Identity // nil while locked
}

// Init creates a vault in dir, which must be missing or empty. A non-empty
// password encrypts it and returns the recovery key, to show once.
func Init(dir, name, password string) (*Vault, string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(dir, MetaFile)); err == nil {
			return nil, "", fmt.Errorf("%s is already a vault", dir)
		}
		return nil, "", fmt.Errorf("%s is not empty", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	if name == "" {
		name = filepath.Base(dir)
		if name == LocalDir {
			name = filepath.Base(filepath.Dir(dir))
		}
	}
	v := &Vault{Dir: dir, Meta: Meta{Format: format, ID: randomID(), Name: name, Created: time.Now().UTC().Truncate(time.Second)}}
	var recovery string
	if password != "" {
		if recovery, err = v.addKeys(password); err != nil {
			return nil, "", err
		}
	}
	for _, d := range []string{ExamsDir, ClassesDir, SessionDir, BackupDir} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			return nil, "", err
		}
	}
	if err := v.saveMeta(); err != nil {
		return nil, "", err
	}
	return v, recovery, nil
}

// Open reads the vault in dir, locked.
func Open(dir string) (*Vault, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, MetaFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotVault)
	}
	if err != nil {
		return nil, err
	}
	v := &Vault{Dir: dir}
	if err := yaml.Unmarshal(data, &v.Meta); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, MetaFile), err)
	}
	if v.Meta.Format > format {
		return nil, fmt.Errorf("%s was made by a newer ax (format %d); upgrade ax", dir, v.Meta.Format)
	}
	if v.Meta.Encrypted && (v.Meta.Recipient == "" || v.Meta.PasswordKey == "") {
		return nil, fmt.Errorf("%s: vault.yaml says encrypted but holds no key", dir)
	}
	return v, nil
}

// IsVault reports whether dir holds a vault.
func IsVault(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, MetaFile))
	return err == nil
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

const metaHeader = `# Assess vault. Copy the whole folder to move it to another computer.
# Do not edit by hand: the key below is wrapped with your password.
`

func (v *Vault) saveMeta() error {
	data, err := yaml.Marshal(&v.Meta)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(v.Dir, MetaFile), append([]byte(metaHeader), data...))
}

// SetName changes the vault's display name.
func (v *Vault) SetName(name string) error {
	v.Meta.Name = name
	return v.saveMeta()
}

// DB is the database file.
func (v *Vault) DB() string { return filepath.Join(v.Dir, DBFile) }

// Encrypted reports whether the vault is encrypted.
func (v *Vault) Encrypted() bool { return v.Meta.Encrypted }

// Locked reports whether the key is needed and missing.
func (v *Vault) Locked() bool { return v.Meta.Encrypted && v.id == nil }

// ---- keys ----

// addKeys makes a fresh identity, wraps it with password and a new
// recovery key, and marks the vault encrypted. It returns the recovery key.
func (v *Vault) addKeys(password string) (string, error) {
	if len([]rune(password)) < MinPassword {
		return "", ErrShortPassword
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	pw, err := wrapPassword(id, password)
	if err != nil {
		return "", err
	}
	rk, err := wrap(id, rec.Recipient())
	if err != nil {
		return "", err
	}
	v.id = id
	v.Meta.Encrypted, v.Meta.Recipient, v.Meta.PasswordKey, v.Meta.RecoveryKey = true, id.Recipient().String(), pw, rk
	return rec.String(), nil
}

func wrapPassword(id *age.X25519Identity, password string) (string, error) {
	r, err := age.NewScryptRecipient(password)
	if err != nil {
		return "", err
	}
	r.SetWorkFactor(ScryptWorkFactor)
	return wrap(id, r)
}

func wrap(id *age.X25519Identity, r age.Recipient) (string, error) {
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, r)
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(w, id.String()); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	if err := aw.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func unwrap(wrapped string, id age.Identity) (*age.X25519Identity, error) {
	r, err := age.Decrypt(armor.NewReader(strings.NewReader(wrapped)), id)
	if err != nil {
		var nm *age.NoIdentityMatchError
		if errors.As(err, &nm) || strings.Contains(err.Error(), "incorrect passphrase") {
			return nil, ErrWrongPassword
		}
		return nil, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, ErrWrongPassword
	}
	return age.ParseX25519Identity(strings.TrimSpace(string(data)))
}

// Unlock opens the key with the password.
func (v *Vault) Unlock(password string) error {
	if !v.Meta.Encrypted {
		return nil
	}
	sid, err := age.NewScryptIdentity(password)
	if err != nil {
		return err
	}
	id, err := unwrap(v.Meta.PasswordKey, sid)
	if err != nil {
		return err
	}
	return v.setIdentity(id)
}

// CheckPassword reports whether password opens the vault, without
// keeping the key.
func (v *Vault) CheckPassword(password string) bool {
	probe := &Vault{Meta: v.Meta}
	return probe.Unlock(password) == nil
}

// UnlockRecovery opens the key with the recovery key.
func (v *Vault) UnlockRecovery(recovery string) error {
	if !v.Meta.Encrypted {
		return nil
	}
	rid, err := age.ParseX25519Identity(strings.TrimSpace(recovery))
	if err != nil {
		return errors.New("that is not a recovery key (it starts with AGE-SECRET-KEY-1)")
	}
	if v.Meta.RecoveryKey == "" {
		return ErrWrongPassword
	}
	id, err := unwrap(v.Meta.RecoveryKey, rid)
	if err != nil {
		return err
	}
	return v.setIdentity(id)
}

// UnlockWith opens the vault with an identity kept by the unlock agent.
func (v *Vault) UnlockWith(identity string) error {
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return err
	}
	return v.setIdentity(id)
}

func (v *Vault) setIdentity(id *age.X25519Identity) error {
	if id.Recipient().String() != v.Meta.Recipient {
		return errors.New("that key belongs to another vault")
	}
	v.id = id
	return nil
}

// Identity is the unlocked key, for the unlock agent.
func (v *Vault) Identity() string {
	if v.id == nil {
		return ""
	}
	return v.id.String()
}

// Lock forgets the key.
func (v *Vault) Lock() { v.id = nil }

// SetPassword replaces the password. The vault must be unlocked.
func (v *Vault) SetPassword(password string) error {
	if v.Locked() || !v.Meta.Encrypted {
		return ErrLocked
	}
	if len([]rune(password)) < MinPassword {
		return ErrShortPassword
	}
	pw, err := wrapPassword(v.id, password)
	if err != nil {
		return err
	}
	v.Meta.PasswordKey = pw
	return v.saveMeta()
}

// DBKey is the key that seals the database columns; nil for a plain vault.
func (v *Vault) DBKey() ([]byte, error) {
	if !v.Meta.Encrypted {
		return nil, nil
	}
	if v.id == nil {
		return nil, ErrLocked
	}
	return hkdf.Key(sha256.New, []byte(v.id.String()), []byte(v.Meta.ID), "assess/vault/db", seal.KeySize)
}

// Cipher is the database cipher; nil for a plain vault.
func (v *Vault) Cipher() (*seal.Cipher, error) {
	k, err := v.DBKey()
	if err != nil || k == nil {
		return nil, err
	}
	return seal.New(k)
}

// ---- files ----

// path of a logical file name ("exams/x.examx") on disk, plain.
func (v *Vault) path(name string) string {
	return filepath.Join(v.Dir, filepath.FromSlash(name))
}

// Exists reports whether the logical file exists, encrypted or not.
func (v *Vault) Exists(name string) bool {
	for _, p := range []string{v.path(name), v.path(name) + Ext} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// WriteFile stores data under the logical name; in an encrypted vault the
// file is encrypted to the vault, which does not need the password.
func (v *Vault) WriteFile(name string, data []byte) error {
	plain, enc := v.path(name), v.path(name)+Ext
	if err := os.MkdirAll(filepath.Dir(plain), 0o700); err != nil {
		return err
	}
	if !v.Meta.Encrypted {
		if err := writeAtomic(plain, data); err != nil {
			return err
		}
		return removeIfExists(enc)
	}
	sealed, err := v.encrypt(data)
	if err != nil {
		return err
	}
	if err := writeAtomic(enc, sealed); err != nil {
		return err
	}
	return removeIfExists(plain)
}

func (v *Vault) encrypt(data []byte) ([]byte, error) {
	r, err := age.ParseX25519Recipient(v.Meta.Recipient)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (v *Vault) decrypt(data []byte) ([]byte, error) {
	if v.id == nil {
		return nil, ErrLocked
	}
	r, err := age.Decrypt(bytes.NewReader(data), v.id)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// ReadFile returns the contents of a logical file.
func (v *Vault) ReadFile(name string) ([]byte, error) {
	if data, err := os.ReadFile(v.path(name) + Ext); err == nil {
		return v.decrypt(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.ReadFile(v.path(name))
}

// Remove deletes a logical file.
func (v *Vault) Remove(name string) error {
	return errors.Join(removeIfExists(v.path(name)), removeIfExists(v.path(name)+Ext))
}

// Rename moves a logical file.
func (v *Vault) Rename(from, to string) error {
	data, err := v.ReadFile(from)
	if err != nil {
		return err
	}
	if err := v.WriteFile(to, data); err != nil {
		return err
	}
	return v.Remove(from)
}

// List returns the logical names of the files under dir (a logical folder
// such as "exams"), sorted.
func (v *Vault) List(dir string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(v.path(dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(v.Dir, p)
		if err != nil {
			return err
		}
		seen[strings.TrimSuffix(filepath.ToSlash(rel), Ext)] = true
		return nil
	})
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, err
}

// Files lists every logical file of the vault except vault.yaml, the
// database and the backups.
func (v *Vault) Files() ([]string, error) {
	var out []string
	for _, d := range []string{ExamsDir, ClassesDir, SessionDir} {
		names, err := v.List(d)
		if err != nil {
			return nil, err
		}
		out = append(out, names...)
	}
	return out, nil
}

// ---- encrypt and decrypt ----

// Encrypt turns a plain vault into an encrypted one: new keys, then every
// file is encrypted. It returns the recovery key. Running it again on an
// encrypted, unlocked vault finishes an interrupted conversion. The
// database is resealed by the caller (sqlite.Reseal) with Cipher().
func (v *Vault) Encrypt(password string) (string, error) {
	var recovery string
	if !v.Meta.Encrypted {
		var err error
		if recovery, err = v.addKeys(password); err != nil {
			return "", err
		}
		if err := v.saveMeta(); err != nil {
			return "", err
		}
	} else if v.Locked() {
		return "", ErrLocked
	}
	return recovery, v.rewriteFiles()
}

// Decrypt turns an unlocked encrypted vault into a plain one. Reseal the
// database to plain first.
func (v *Vault) Decrypt() error {
	if !v.Meta.Encrypted {
		return nil
	}
	if v.Locked() {
		return ErrLocked
	}
	id := v.id
	names, err := v.Files()
	if err != nil {
		return err
	}
	v.Meta.Encrypted = false
	for _, n := range names {
		v.id = id
		data, err := v.ReadFile(n)
		if err != nil {
			v.Meta.Encrypted = true
			return fmt.Errorf("%s: %w", n, err)
		}
		if err := v.WriteFile(n, data); err != nil {
			v.Meta.Encrypted = true
			return err
		}
	}
	v.Meta.Recipient, v.Meta.PasswordKey, v.Meta.RecoveryKey = "", "", ""
	v.id = nil
	return v.saveMeta()
}

// rewriteFiles writes every file again in the vault's current form.
func (v *Vault) rewriteFiles() error {
	names, err := v.Files()
	if err != nil {
		return err
	}
	for _, n := range names {
		if v.Meta.Encrypted {
			if _, err := os.Stat(v.path(n)); err != nil {
				continue // already encrypted
			}
		}
		data, err := v.ReadFile(n)
		if err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
		if err := v.WriteFile(n, data); err != nil {
			return err
		}
	}
	return nil
}

// ---- helpers ----

func removeIfExists(p string) error {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeAtomic(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// SafeName turns a title into a file name: no separators or characters
// Windows rejects, at most 80 characters.
func SafeName(s string) string {
	r := strings.NewReplacer("/", "-", `\`, "-", ":", "-", "*", "", "?", "", `"`, "", "<", "", ">", "", "|", "", "\x00", "")
	s = strings.TrimSpace(strings.Join(strings.Fields(r.Replace(s)), " "))
	s = strings.Trim(s, ". ")
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
	}
	if s == "" {
		s = "untitled"
	}
	return s
}

// Join builds a logical name.
func Join(parts ...string) string { return path.Join(parts...) }
