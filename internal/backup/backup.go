// Package backup copies a whole vault into one ZIP file and restores it.
// The database is copied with VACUUM INTO, so a backup can be taken while
// an exam is running. An encrypted vault's backup stays encrypted: its
// files are copied as they are on disk.
package backup

import (
	"archive/zip"
	"context"
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

	"github.com/lisk0vian/assess/internal/storage/sqlite"
	"github.com/lisk0vian/assess/internal/vault"
)

// Prefixes of backup file names: made by `ax backup`, or automatically
// when a session closes (those are pruned).
const (
	ManualPrefix = "backup-"
	AutoPrefix   = "auto-"
	Ext          = ".zip"
)

// Limits for restoring a file of unknown origin.
const (
	maxFiles = 100000
	maxTotal = 8 << 30
)

// Create writes a backup of v into dest (a file, or a folder for a dated
// name; empty means the vault's backups/ folder). db is the open database
// of the vault, or nil to copy the file when no server is using it.
func Create(ctx context.Context, v *vault.Vault, db *sqlite.DB, dest, prefix string) (string, error) {
	if prefix == "" {
		prefix = ManualPrefix
	}
	if dest == "" {
		dest = filepath.Join(v.Dir, vault.BackupDir)
	}
	if info, err := os.Stat(dest); (err == nil && info.IsDir()) || strings.HasSuffix(dest, string(filepath.Separator)) {
		dest = filepath.Join(dest, prefix+time.Now().Format("2006-01-02-150405")+Ext)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".backup-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	zw := zip.NewWriter(tmp)
	backups, _ := filepath.Abs(filepath.Join(v.Dir, vault.BackupDir))
	err = filepath.WalkDir(v.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if vault.SamePath(p, backups) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, vault.DBFile) {
			return nil // temporary files; the database is copied below
		}
		rel, err := filepath.Rel(v.Dir, p)
		if err != nil {
			return err
		}
		return addFile(zw, filepath.ToSlash(rel), p)
	})
	if err == nil {
		err = addDB(ctx, zw, v, db)
	}
	if err == nil {
		err = zw.Close()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", err
	}
	return dest, nil
}

func addFile(zw *zip.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	h.Name, h.Method = name, zip.Deflate
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func addDB(ctx context.Context, zw *zip.Writer, v *vault.Vault, db *sqlite.DB) error {
	if _, err := os.Stat(v.DB()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	tmpDir, err := os.MkdirTemp(filepath.Join(v.Dir, vault.BackupDir), ".db-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	snap := filepath.Join(tmpDir, vault.DBFile)
	if db == nil {
		// Open the file only for the copy; WAL keeps it consistent.
		if db, err = sqlite.Open(ctx, v.DB()); err != nil {
			return err
		}
		defer db.Close()
	}
	if err := db.BackupTo(ctx, snap); err != nil {
		return err
	}
	return addFile(zw, vault.DBFile, snap)
}

// Prune keeps the newest keep automatic backups of v.
func Prune(v *vault.Vault, keep int) error {
	dir := filepath.Join(v.Dir, vault.BackupDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var auto []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), AutoPrefix) && strings.HasSuffix(e.Name(), Ext) {
			auto = append(auto, e.Name())
		}
	}
	sort.Strings(auto) // dated names sort by time
	var errs []error
	for len(auto) > keep {
		errs = append(errs, os.Remove(filepath.Join(dir, auto[0])))
		auto = auto[1:]
	}
	return errors.Join(errs...)
}

// List returns the backups of v, newest first.
func List(v *vault.Vault) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(v.Dir, vault.BackupDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), Ext) && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(v.Dir, vault.BackupDir, e.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := os.Stat(out[i])
		b, _ := os.Stat(out[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	return out, nil
}

// Restore unpacks a backup into dest, which must be missing or empty, and
// returns the restored vault (locked).
func Restore(src, dest string) (*vault.Vault, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return nil, fmt.Errorf("%s is not a backup: %w", src, err)
	}
	defer func() { _ = zr.Close() }()
	hasMeta := false
	for _, f := range zr.File {
		if f.Name == vault.MetaFile {
			hasMeta = true
		}
	}
	if !hasMeta {
		return nil, fmt.Errorf("%s is not a vault backup (no %s)", src, vault.MetaFile)
	}
	if len(zr.File) > maxFiles {
		return nil, errors.New("the backup holds too many files")
	}
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty; restore into a new folder", dest)
	}
	root, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	var total int64
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		if f.Name == "" || strings.Contains(f.Name, `\`) || strings.Contains(f.Name, ":") || path.IsAbs(f.Name) ||
			path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "../") {
			return nil, fmt.Errorf("unsafe file name in the backup: %q", f.Name)
		}
		out := filepath.Join(root, filepath.FromSlash(f.Name))
		if rel, err := filepath.Rel(root, out); err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("unsafe file name in the backup: %q", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return nil, err
		}
		n, err := extract(f, out, maxTotal-total)
		if err != nil {
			return nil, err
		}
		total += n
	}
	for _, d := range []string{vault.ExamsDir, vault.ClassesDir, vault.SessionDir, vault.BackupDir} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o700)
	}
	return vault.Open(root)
}

func extract(f *zip.File, out string, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	w, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, io.LimitReader(rc, budget+1))
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > budget {
		err = errors.New("the backup is too large")
	}
	return n, err
}
