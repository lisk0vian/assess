package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Source says how the active vault was chosen.
type Source string

// Sources, in order of precedence.
const (
	FromFlag    Source = "flag"    // --vault
	FromFolder  Source = "folder"  // .assess-vault/ here or in a parent folder
	FromSetting Source = "setting" // vault.path (AX_VAULT_PATH, `ax vault use`)
	FromDefault Source = "default" // the vault of the data folder
)

// Entry is a vault known by name.
type Entry struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Registry is vaults.yaml, next to the settings file: the vaults the
// teacher created or added, by name. Forgetting a vault there never
// deletes it.
type Registry struct {
	path   string
	Vaults []Entry `yaml:"vaults"`
}

// LoadRegistry reads the registry at path; a missing file is empty.
func LoadRegistry(path string) (*Registry, error) {
	r := &Registry{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Paths are kept relative to the registry when they are below it, so a
	// portable copy keeps working when the USB stick gets another letter.
	for i, e := range r.Vaults {
		if !filepath.IsAbs(e.Path) {
			r.Vaults[i].Path = filepath.Join(filepath.Dir(path), e.Path)
		}
	}
	return r, nil
}

// Save writes the registry.
func (r *Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	out := Registry{Vaults: make([]Entry, len(r.Vaults))}
	base := filepath.Dir(r.path)
	for i, e := range r.Vaults {
		out.Vaults[i] = e
		if rel, err := filepath.Rel(base, e.Path); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			out.Vaults[i].Path = rel
		}
	}
	data, err := yaml.Marshal(&out)
	if err != nil {
		return err
	}
	return writeAtomic(r.path, append([]byte("# Vaults known to ax, by name. `ax vault list` shows them.\n"), data...))
}

// Find returns the entry with name (case-insensitive) or path.
func (r *Registry) Find(nameOrPath string) (Entry, bool) {
	abs, _ := filepath.Abs(nameOrPath)
	for _, e := range r.Vaults {
		if strings.EqualFold(e.Name, nameOrPath) || samePath(e.Path, abs) {
			return e, true
		}
	}
	return Entry{}, false
}

// Add remembers a vault; a name already taken by another path gets a
// number appended. It returns the name used.
func (r *Registry) Add(name, path string) string {
	abs, _ := filepath.Abs(path)
	if e, ok := r.Find(abs); ok {
		return e.Name
	}
	base, n := name, 2
	for {
		if _, taken := r.Find(name); !taken {
			break
		}
		name = fmt.Sprintf("%s-%d", base, n)
		n++
	}
	r.Vaults = append(r.Vaults, Entry{Name: name, Path: abs})
	return name
}

// Forget removes a vault from the list; the folder stays.
func (r *Registry) Forget(name string) bool {
	i := slices.IndexFunc(r.Vaults, func(e Entry) bool { return strings.EqualFold(e.Name, name) })
	if i < 0 {
		return false
	}
	r.Vaults = slices.Delete(r.Vaults, i, i+1)
	return true
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// SamePath reports whether two paths name the same folder.
func SamePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return samePath(a, b)
}

// DefaultDir is the vault of a data folder, created on first use.
func DefaultDir(dataDir string) string { return filepath.Join(dataDir, "vault") }

// Choice is how the active vault is found.
type Choice struct {
	Flag     string    // --vault: a name or a path
	Setting  string    // vault.path: a name or a path
	Cwd      string    // where to look for .assess-vault/
	Base     string    // relative paths in Setting are relative to it (the settings file's folder)
	DataDir  string    // for the default vault
	Registry *Registry // names
}

// Locate returns the folder of the active vault and how it was chosen. The
// folder may not exist yet when it is the default.
func Locate(c Choice) (string, Source, error) {
	resolve := func(v string) (string, error) {
		if c.Registry != nil {
			if e, ok := c.Registry.Find(v); ok {
				return e.Path, nil
			}
		}
		if c.Base != "" && !filepath.IsAbs(v) {
			if p := filepath.Join(c.Base, v); IsVault(p) || IsVault(filepath.Join(p, LocalDir)) {
				v = p
			}
		}
		if IsVault(v) {
			return filepath.Abs(v)
		}
		if IsVault(filepath.Join(v, LocalDir)) {
			return filepath.Abs(filepath.Join(v, LocalDir))
		}
		return "", fmt.Errorf("no vault named or found at %q (see `ax vault list`)", v)
	}
	if c.Flag != "" {
		p, err := resolve(c.Flag)
		return p, FromFlag, err
	}
	if c.Cwd != "" {
		if p := findUp(c.Cwd); p != "" {
			return p, FromFolder, nil
		}
	}
	if c.Setting != "" {
		p, err := resolve(c.Setting)
		return p, FromSetting, err
	}
	return DefaultDir(c.DataDir), FromDefault, nil
}

// findUp looks for .assess-vault/ in dir and its parents.
func findUp(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, LocalDir)
		if IsVault(p) {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
