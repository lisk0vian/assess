// Package examx packs an exam folder into one .examx file and unpacks it
// safely. An .examx is a ZIP holding the exam's files and examx.json, which
// lists the SHA-256 of each one. It never holds student data.
//
// A package shared outside a vault can be encrypted with a passphrase
// (age, scrypt); Seal and Open do that, and Open recognises both forms.
package examx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	"github.com/lisk0vian/assess/internal/exam"
)

// Ext is the file extension.
const Ext = ".examx"

// IndexFile is the table of contents inside the ZIP.
const IndexFile = "examx.json"

// Format is the package format version.
const Format = 1

// Limits that keep a hostile package from filling the disk.
const (
	MaxFiles     = 2000
	MaxFileSize  = 32 << 20  // one file, uncompressed
	MaxTotalSize = 256 << 20 // the whole exam, uncompressed
)

// keyDir is the folder of the answer key, left out by NoKey.
const keyDir = "private"

// Index is examx.json.
type Index struct {
	Format  int               `json:"format"`
	ExamID  string            `json:"exam_id"`
	Title   string            `json:"title"`
	HasKey  bool              `json:"has_key"`
	Created time.Time         `json:"created"`
	Files   map[string]string `json:"files"` // slash path → hex SHA-256
}

// Options tune Pack.
type Options struct {
	NoKey bool // leave out private/ (the answer key)
}

// Pack zips the exam folder at dir. The exam must load (it may have
// warnings); hidden files are left out.
func Pack(dir string, opts Options) ([]byte, *Index, error) {
	ex, _, err := exam.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	idx := &Index{Format: Format, ExamID: ex.Manifest.ID, Title: ex.Manifest.Title, HasKey: !opts.NoKey,
		Created: time.Now().UTC().Truncate(time.Second), Files: map[string]string{}}
	var names []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || (opts.NoKey && (rel == keyDir || strings.HasPrefix(rel, keyDir+"/"))) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			names = append(names, rel)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(names) > MaxFiles {
		return nil, nil, fmt.Errorf("the exam has %d files; the limit is %d", len(names), MaxFiles)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var total int64
	for _, rel := range names {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, err
		}
		total += int64(len(data))
		if len(data) > MaxFileSize || total > MaxTotalSize {
			return nil, nil, fmt.Errorf("%s: the exam is too large to package", rel)
		}
		sum := sha256.Sum256(data)
		idx.Files[rel] = hex.EncodeToString(sum[:])
		w, err := zw.CreateHeader(&zip.FileHeader{Name: rel, Method: zip.Deflate, Modified: idx.Created})
		if err != nil {
			return nil, nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, nil, err
		}
	}
	meta, _ := json.MarshalIndent(idx, "", "  ")
	w, err := zw.CreateHeader(&zip.FileHeader{Name: IndexFile, Method: zip.Deflate, Modified: idx.Created})
	if err != nil {
		return nil, nil, err
	}
	if _, err := w.Write(meta); err != nil {
		return nil, nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), idx, nil
}

// Contents is an unpacked, verified package held in memory.
type Contents struct {
	Index *Index
	Files map[string][]byte
}

// ErrNotPackage is returned for data that is not an .examx.
var ErrNotPackage = errors.New("not an .examx package")

// Read verifies a package: safe names, limits, and every file matching its
// hash in examx.json. Nothing is written.
func Read(data []byte) (*Contents, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrNotPackage
	}
	if len(zr.File) > MaxFiles+1 {
		return nil, fmt.Errorf("the package has %d files; the limit is %d", len(zr.File), MaxFiles)
	}
	c := &Contents{Files: map[string][]byte{}}
	var total int64
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue // a folder entry
		}
		if err := safeName(f.Name); err != nil {
			return nil, err
		}
		if f.UncompressedSize64 > MaxFileSize {
			return nil, fmt.Errorf("%s is too large", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		// Never trust the declared size: read at most the limit plus one.
		b, err := io.ReadAll(io.LimitReader(rc, MaxFileSize+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		if len(b) > MaxFileSize {
			return nil, fmt.Errorf("%s is too large", f.Name)
		}
		if total += int64(len(b)); total > MaxTotalSize {
			return nil, errors.New("the package is too large")
		}
		if _, dup := c.Files[f.Name]; dup {
			return nil, fmt.Errorf("%s appears twice", f.Name)
		}
		c.Files[f.Name] = b
	}
	meta, ok := c.Files[IndexFile]
	if !ok {
		return nil, ErrNotPackage
	}
	delete(c.Files, IndexFile)
	c.Index = &Index{}
	if err := json.Unmarshal(meta, c.Index); err != nil {
		return nil, fmt.Errorf("%s: %w", IndexFile, err)
	}
	if c.Index.Format > Format {
		return nil, fmt.Errorf("the package was made by a newer ax (format %d); upgrade ax", c.Index.Format)
	}
	for name, want := range c.Index.Files {
		data, ok := c.Files[name]
		if !ok {
			return nil, fmt.Errorf("%s is listed but missing", name)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			return nil, fmt.Errorf("%s does not match its checksum: the package is damaged", name)
		}
	}
	for name := range c.Files {
		if _, ok := c.Index.Files[name]; !ok {
			return nil, fmt.Errorf("%s is not listed in %s", name, IndexFile)
		}
	}
	return c, nil
}

// safeName rejects paths that could land outside the destination folder.
func safeName(name string) error {
	bad := name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || strings.Contains(name, ":") ||
		path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsRune(name, 0)
	if bad {
		return fmt.Errorf("unsafe file name in the package: %q", name)
	}
	return nil
}

// ErrExists is returned by Extract when the folder is not empty.
var ErrExists = errors.New("the folder already exists and is not empty")

// Extract writes the files into dir, which must be missing or empty unless
// force. It validates the exam afterwards and returns its problems.
func (c *Contents) Extract(dir string, force bool) (exam.Problems, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !force {
		return nil, fmt.Errorf("%s: %w", dir, ErrExists)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for name, data := range c.Files {
		dest := filepath.Join(root, filepath.FromSlash(name))
		if rel, err := filepath.Rel(root, dest); err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("unsafe file name in the package: %q", name)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil { //nolint:gosec // G306: exam files are ordinary documents
			return nil, err
		}
	}
	_, probs, err := exam.Load(root)
	return probs, err
}

// Validate unpacks into a temporary folder and validates the exam there.
func (c *Contents) Validate() (exam.Problems, error) {
	tmp, err := os.MkdirTemp("", "ax-examx-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	return c.Extract(tmp, true)
}

// ---- passphrase encryption, for packages shared outside a vault ----

// ErrPassphrase is returned when an encrypted package needs a passphrase.
var ErrPassphrase = errors.New("the package is encrypted; a passphrase is needed")

// ScryptWorkFactor is the cost of the passphrase (2^n); tests lower it.
var ScryptWorkFactor = 18

// IsEncrypted reports whether data is an age-encrypted file.
func IsEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, []byte("age-encryption.org/v1"))
}

// Seal encrypts a package with a passphrase.
func Seal(data []byte, passphrase string) ([]byte, error) {
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	r.SetWorkFactor(ScryptWorkFactor)
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

// Open decrypts a package with passphrase when it is encrypted.
func Open(data []byte, passphrase string) ([]byte, error) {
	if !IsEncrypted(data) {
		return data, nil
	}
	if passphrase == "" {
		return nil, ErrPassphrase
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(bytes.NewReader(data), id)
	if err != nil {
		return nil, errors.New("wrong passphrase")
	}
	return io.ReadAll(io.LimitReader(r, MaxTotalSize*2))
}
