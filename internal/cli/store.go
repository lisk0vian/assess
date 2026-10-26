package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/term"

	"github.com/lisk0vian/assess/internal/archive"
	"github.com/lisk0vian/assess/internal/backup"
	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/datadir"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
	"github.com/lisk0vian/assess/internal/vault"
)

// vaultFlag is the persistent --vault flag: a vault name or folder.
var vaultFlag string

// spawnAgents starts the unlock agent after a password is typed; tests
// turn it off (the test binary cannot be re-executed as ax).
var spawnAgents = true

// unlocked keeps the keys opened by this process, by vault id, so one
// command asks once even with vault.unlock_minutes set to 0.
var unlocked = map[string]string{}

// readSecret asks for a password without echo; tests replace it.
var readSecret = func(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // G115: fds fit in int
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func registryPath(dataDir string) string {
	if cfg.Path != "" {
		return filepath.Join(filepath.Dir(cfg.Path), "vaults.yaml")
	}
	return filepath.Join(dataDir, "vaults.yaml")
}

func loadRegistry(dataDir string) *vault.Registry {
	r, err := vault.LoadRegistry(registryPath(dataDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		r, _ = vault.LoadRegistry("")
	}
	return r
}

// activeVault finds the vault in use, creating the default one (and moving
// the data of an older ax into it) on first use.
func activeVault(dataDir string) (*vault.Vault, vault.Source, error) {
	cwd, _ := os.Getwd()
	reg := loadRegistry(dataDir)
	p, src, err := vault.Locate(vault.Choice{Flag: vaultFlag, Setting: cfg.Str("vault.path"), Cwd: cwd, Base: cfg.Dir(),
		DataDir: dataDir, Registry: reg})
	if err != nil {
		return nil, src, err
	}
	if src == vault.FromDefault && !vault.IsVault(p) {
		if err := createDefault(dataDir, p, reg); err != nil {
			return nil, src, err
		}
	}
	v, err := vault.Open(p)
	return v, src, err
}

// createDefault makes the default vault, plain, and moves the database of
// a previous ax version into it.
func createDefault(dataDir, dir string, reg *vault.Registry) error {
	legacy := datadir.DB(dataDir)
	_, err := os.Stat(legacy)
	hasLegacy := err == nil
	if hasLegacy {
		if h, _, _ := daemon.Check(dataDir); h == daemon.Running {
			return errors.New(T("vault.migrate.running"))
		}
	}
	v, _, err := vault.Init(dir, "default", "")
	if err != nil {
		return err
	}
	reg.Add("default", dir)
	_ = reg.Save()
	if !hasLegacy {
		return nil
	}
	return migrateLegacy(context.Background(), v, legacy)
}

// migrateLegacy copies the old database into the vault, keeps the original
// in the vault's backups and archives the finished sessions.
func migrateLegacy(ctx context.Context, v *vault.Vault, legacy string) error {
	old, err := sqlite.Open(ctx, legacy)
	if err != nil {
		return fmt.Errorf("%s: %w", legacy, err)
	}
	err = old.BackupTo(ctx, v.DB())
	old.Close()
	if err != nil {
		return err
	}
	kept := filepath.Join(v.Dir, vault.BackupDir, "pre-vault-"+time.Now().Format("2006-01-02-150405")+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(legacy + suffix); err == nil {
			if err := os.Rename(legacy+suffix, kept+suffix); err != nil {
				return err
			}
		}
	}
	db, err := sqlite.Open(ctx, v.DB())
	if err != nil {
		return err
	}
	defer db.Close()
	n, err := archive.Missing(ctx, v, session.NewService(db))
	if err != nil {
		fmt.Fprintln(os.Stderr, T("vault.migrate.archive_failed", "err", err))
	}
	fmt.Fprintln(os.Stderr, T("vault.migrate.done", "dir", v.Dir, "copy", kept, "n", n))
	return nil
}

// tryUnlock opens the vault with a key at hand, without asking.
func tryUnlock(ctx context.Context, dataDir string, v *vault.Vault) bool {
	if !v.Locked() {
		return true
	}
	if id := unlocked[v.Meta.ID]; id != "" && v.UnlockWith(id) == nil {
		return true
	}
	if id := vault.AgentKey(ctx, dataDir, v.Meta.ID); id != "" && v.UnlockWith(id) == nil {
		unlocked[v.Meta.ID] = id
		return true
	}
	return false
}

// unlock opens an encrypted vault: with a key this process already has,
// with the unlock agent's, or by asking the password.
func unlock(ctx context.Context, dataDir string, v *vault.Vault) error {
	if tryUnlock(ctx, dataDir, v) {
		return nil
	}
	if !isTerminal() {
		return errors.New(T("vault.locked_no_terminal", "name", v.Meta.Name))
	}
	if err := askPassword(v); err != nil {
		return err
	}
	startAgent(dataDir, v)
	return nil
}

// askPassword asks the vault's password, three tries.
func askPassword(v *vault.Vault) error {
	for range 3 {
		pw, err := readSecret(T("vault.password_prompt", "name", v.Meta.Name))
		if err != nil {
			return err
		}
		if err := v.Unlock(pw); err == nil {
			unlocked[v.Meta.ID] = v.Identity()
			return nil
		} else if !errors.Is(err, vault.ErrWrongPassword) {
			return err
		}
		fmt.Fprintln(os.Stderr, T("vault.wrong_password"))
	}
	return errors.New(T("vault.wrong_password_err"))
}

// confirmPassword asks the password even when the vault is open: for
// forgetting, merging, decrypting, exporting and changing the password.
func confirmPassword(v *vault.Vault) error {
	if !v.Encrypted() {
		return nil
	}
	if !isTerminal() {
		return errors.New(T("vault.locked_no_terminal", "name", v.Meta.Name))
	}
	v.Lock()
	return askPassword(v)
}

// startAgent keeps the vault open for vault.unlock_minutes.
func startAgent(dataDir string, v *vault.Vault) {
	minutes := cfg.Int("vault.unlock_minutes")
	if !spawnAgents || minutes <= 0 {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if err := vault.SpawnAgent(exe, dataDir, v, time.Duration(minutes)*time.Minute, daemon.Detach); err != nil {
		fmt.Fprintln(os.Stderr, T("vault.agent_failed", "err", err))
	}
}

// lockVaults forgets every open key: the agents' and this process's.
func lockVaults(ctx context.Context, dataDir string) {
	vault.LockAllAgents(ctx, dataDir)
	clear(unlocked)
}

// store is an open vault and its database.
type store struct {
	dir   string // data folder
	vault *vault.Vault
	db    *sqlite.DB
	svc   *session.Service
}

func (s *store) Close() { _ = s.db.Close() }

// openStore opens the active vault's database; needKey unlocks an
// encrypted vault first (asking the password if needed). Without the key
// only plain columns can be read.
func openStore(ctx context.Context, dataDir string, needKey bool) (*store, error) {
	v, _, err := activeVault(dataDir)
	if err != nil {
		return nil, err
	}
	if needKey {
		if err := unlock(ctx, dataDir, v); err != nil {
			return nil, err
		}
	} else {
		tryUnlock(ctx, dataDir, v)
	}
	c, err := v.Cipher()
	if err != nil && !errors.Is(err, vault.ErrLocked) {
		return nil, err
	}
	db, err := sqlite.Open(ctx, v.DB(), sqlite.WithCipher(c, v.Encrypted()))
	if err != nil {
		return nil, err
	}
	return &store{dir: dataDir, vault: v, db: db, svc: session.NewService(db)}, nil
}

// withService opens the active vault's database for a short operation.
func withService(ctx context.Context, dir string, fn func(*session.Service) error) error {
	return withStore(ctx, dir, func(s *store) error { return fn(s.svc) })
}

// withStore is withService with the vault at hand.
func withStore(ctx context.Context, dir string, fn func(*store) error) error {
	s, err := openStore(ctx, dir, true)
	if err != nil {
		return err
	}
	defer s.Close()
	return fn(s)
}

// withLockedService reads plain columns without asking for the password.
func withLockedService(ctx context.Context, dir string, fn func(*session.Service) error) error {
	s, err := openStore(ctx, dir, false)
	if err != nil {
		return err
	}
	defer s.Close()
	return fn(s.svc)
}

// daemonKey is what the exam server needs to open the vault's database:
// its folder, and the key when encrypted (handed over through a pipe).
func daemonKey(ctx context.Context, dataDir string) (string, []byte, error) {
	v, _, err := activeVault(dataDir)
	if err != nil {
		return "", nil, err
	}
	if err := unlock(ctx, dataDir, v); err != nil {
		return "", nil, err
	}
	k, err := v.DBKey()
	return v.Dir, k, err
}

// readKey reads a database key from r (the daemon's standard input).
func readKey(r io.Reader) ([]byte, error) {
	buf := make([]byte, 64)
	n, err := io.ReadFull(r, buf[:32])
	if err != nil {
		return nil, fmt.Errorf("reading the vault key: %w", err)
	}
	return buf[:n], nil
}

// closeArchive writes the archive of a session that ended without a
// server, and an automatic backup.
func closeArchive(ctx context.Context, s *store, sessionID string) {
	if _, err := archive.Write(ctx, s.vault, s.svc, sessionID); err != nil {
		fmt.Fprintln(os.Stderr, T("people.archive_failed", "err", err))
		return
	}
	if keep := cfg.Int("vault.keep_backups"); keep > 0 {
		if _, err := backup.Create(ctx, s.vault, s.db, "", backup.AutoPrefix); err == nil {
			_ = backup.Prune(s.vault, keep)
		}
	}
}
