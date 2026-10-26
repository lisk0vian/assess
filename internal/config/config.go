// Package config reads and writes ax's settings file,
// ~/.config/assess/config.yaml on every system.
//
// Every setting has a built-in default; the file only holds what the teacher
// changed. A value is taken from, in order: a command-line flag (applied by
// the CLI), the environment (AX_<KEY>, for example AX_SERVER_PORT), the
// file, the default. Keys are described once, in Keys, which drives
// validation, `ax config`, the Settings screen and the commented file
// written on the first run.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/lisk0vian/assess/internal/i18n"
)

// EnvVar overrides the location of the settings file.
const EnvVar = "AX_CONFIG"

// Kind is the type of a setting's value.
type Kind int

// Kinds of values.
const (
	String Kind = iota
	Int
	Bool
	Duration
	Choice
)

// Key describes one setting.
type Key struct {
	Name    string   // dotted: "server.port"
	Kind    Kind     //
	Default string   // as written in the file
	Choices []string // for Choice
	Min     int      // for Int
	Max     int      // for Int, when > 0
	// Check validates a value beyond its kind.
	Check func(string) error
}

// Group is the part before the dot ("" for top-level keys).
func (k Key) Group() string {
	g, _, ok := strings.Cut(k.Name, ".")
	if !ok {
		return ""
	}
	return g
}

// Leaf is the part after the dot.
func (k Key) Leaf() string {
	_, l, ok := strings.Cut(k.Name, ".")
	if !ok {
		return k.Name
	}
	return l
}

// Env is the environment variable that overrides the key.
func (k Key) Env() string {
	return "AX_" + strings.ToUpper(strings.ReplaceAll(k.Name, ".", "_"))
}

// Validate checks a value written as text.
func (k Key) Validate(v string) error {
	switch k.Kind {
	case Int:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%q is not a whole number", v)
		}
		if n < k.Min || (k.Max > 0 && n > k.Max) {
			if k.Max > 0 {
				return fmt.Errorf("%d is out of range (%d–%d)", n, k.Min, k.Max)
			}
			return fmt.Errorf("%d is below %d", n, k.Min)
		}
	case Bool:
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("%q is not true or false", v)
		}
	case Duration:
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%q is not a duration (examples: 90s, 15m, 1h30m)", v)
		}
		if d < 0 {
			return errors.New("must not be negative")
		}
	case Choice:
		if !slices.Contains(k.Choices, v) {
			return fmt.Errorf("%q is not one of %s", v, strings.Join(k.Choices, ", "))
		}
	}
	if k.Check != nil {
		return k.Check(v)
	}
	return nil
}

// accept is Validate, letting text settings be empty.
func (k Key) accept(v string) error {
	if v == "" && k.Kind == String {
		return nil
	}
	return k.Validate(v)
}

// Keys is every setting, in the order of the file.
var Keys = []Key{
	{Name: "language", Kind: String, Default: "en", Check: checkLang},
	{Name: "student_language", Kind: String, Check: checkLang},
	{Name: "data_dir", Kind: String},
	{Name: "editor", Kind: String},

	{Name: "server.port", Kind: Int, Default: "27337", Min: 1, Max: 65535},
	{Name: "server.host", Kind: String},
	{Name: "server.join_window", Kind: Duration, Default: "15m"},
	{Name: "server.late_answers", Kind: Duration, Default: "30s"},
	{Name: "server.allow_public", Kind: Bool, Default: "false"},

	{Name: "new_exam.duration", Kind: Duration, Default: "45m", Check: func(v string) error {
		if d, _ := time.ParseDuration(v); d < time.Minute {
			return errors.New("must be at least 1m")
		}
		return nil
	}},
	{Name: "new_exam.result_visibility", Kind: Choice, Default: "score", Choices: []string{"none", "score", "review"}},
	{Name: "new_exam.proctoring", Kind: Choice, Default: "standard", Choices: []string{"off", "standard", "strict"}},
	{Name: "new_exam.shuffle_questions", Kind: Bool, Default: "true"},
	{Name: "new_exam.shuffle_options", Kind: Bool, Default: "true"},
	{Name: "new_exam.instructions", Kind: String},

	{Name: "panel.lock_minutes", Kind: Int, Default: "5", Min: 0, Max: 1440},

	{Name: "results.format", Kind: Choice, Default: "csv", Choices: []string{"csv", "json"}},
	{Name: "results.separator", Kind: String, Default: ",", Check: func(v string) error {
		if len([]rune(v)) != 1 {
			return errors.New("must be a single character")
		}
		return nil
	}},
	{Name: "results.bom", Kind: Bool, Default: "true"},

	{Name: "vault.path", Kind: String},
	{Name: "vault.encrypt", Kind: Bool, Default: "true"},
	{Name: "vault.unlock_minutes", Kind: Int, Default: "15", Min: 0, Max: 1440},
	{Name: "vault.keep_backups", Kind: Int, Default: "10", Min: 0, Max: 1000},
}

func checkLang(v string) error {
	if !i18n.ValidLang(v) {
		return fmt.Errorf("%q is not a language code (examples: en, es, pt-BR)", v)
	}
	if !i18n.Has(v) {
		var have []string
		for _, c := range i18n.Languages() {
			have = append(have, c.Lang)
		}
		return fmt.Errorf("no translation for %q (available: %s)", v, strings.Join(have, ", "))
	}
	return nil
}

// Lookup finds a key by name.
func Lookup(name string) (Key, bool) {
	for _, k := range Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// Groups lists the group names in file order ("" first).
func Groups() []string {
	var out []string
	for _, k := range Keys {
		if !slices.Contains(out, k.Group()) {
			out = append(out, k.Group())
		}
	}
	return out
}

// Path returns the settings file: flag, then $AX_CONFIG, then
// $XDG_CONFIG_HOME/assess/config.yaml, then ~/.config/assess/config.yaml.
func Path(flag string) (string, error) {
	p := flag
	if p == "" {
		p = os.Getenv(EnvVar)
	}
	if p == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", errors.New("cannot find the home directory; pass --config")
			}
			base = filepath.Join(home, ".config")
		}
		p = filepath.Join(base, "assess", "config.yaml")
	}
	return filepath.Abs(p)
}

// Source says where a value came from.
type Source string

// Sources of a value.
const (
	FromDefault Source = "default"
	FromFile    Source = "file"
	FromEnv     Source = "env"
)

// Value is a setting's effective value.
type Value struct {
	Key    Key
	Raw    string
	Source Source
	Line   int // in the file, when Source is FromFile
}

// Problem is something wrong in the file.
type Problem struct {
	File    string
	Line    int
	Key     string
	Message string
	Warning bool // unknown keys: reported, not fatal
}

func (p Problem) Error() string {
	sev := "error"
	if p.Warning {
		sev = "warning"
	}
	if p.Key == "" {
		return fmt.Sprintf("%s:%d: %s: %s", p.File, p.Line, sev, p.Message)
	}
	return fmt.Sprintf("%s:%d: %s: %s: %s", p.File, p.Line, sev, p.Key, p.Message)
}

// Config is the loaded settings.
type Config struct {
	Path     string
	Exists   bool
	Problems []Problem
	values   map[string]Value
}

// HasErrors reports whether any problem is an error (not a warning).
func (c *Config) HasErrors() bool {
	return slices.ContainsFunc(c.Problems, func(p Problem) bool { return !p.Warning })
}

// Defaults is a configuration with no file: built-in values and the
// environment.
func Defaults() *Config {
	c := &Config{values: map[string]Value{}}
	c.fill()
	return c
}

// Load reads the file at path. A missing file is not an error. Invalid
// values are reported in Problems and replaced by their defaults.
func Load(path string) (*Config, error) {
	c := &Config{Path: path, values: map[string]Value{}}
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return nil, err
	default:
		c.Exists = true
		c.parse(data)
	}
	c.fill()
	return c, nil
}

func (c *Config) parse(data []byte) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		line := 1
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		if l, rest, ok := strings.Cut(strings.TrimPrefix(msg, "line "), ":"); ok {
			if n, err := strconv.Atoi(l); err == nil {
				line, msg = n, strings.TrimSpace(rest)
			}
		}
		c.Problems = append(c.Problems, Problem{File: c.Path, Line: line, Message: "not valid YAML: " + msg})
		return
	}
	if len(doc.Content) == 0 {
		return
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		return
	}
	if root.Kind != yaml.MappingNode {
		c.Problems = append(c.Problems, Problem{File: c.Path, Line: root.Line, Message: "expected settings as `key: value` lines"})
		return
	}
	groups := Groups()
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if key, ok := Lookup(k.Value); ok && key.Group() == "" {
			c.set(key, v)
			continue
		}
		if !slices.Contains(groups, k.Value) || k.Value == "" {
			c.Problems = append(c.Problems, Problem{File: c.Path, Line: k.Line, Key: k.Value, Message: "unknown setting (ignored)", Warning: true})
			continue
		}
		if v.Kind == yaml.ScalarNode && (v.Tag == "!!null" || v.Value == "") {
			continue // a group with every key commented out
		}
		if v.Kind != yaml.MappingNode {
			c.Problems = append(c.Problems, Problem{File: c.Path, Line: v.Line, Key: k.Value, Message: "expected a group of settings below it"})
			continue
		}
		for j := 0; j+1 < len(v.Content); j += 2 {
			name := k.Value + "." + v.Content[j].Value
			key, ok := Lookup(name)
			if !ok {
				c.Problems = append(c.Problems, Problem{File: c.Path, Line: v.Content[j].Line, Key: name, Message: "unknown setting (ignored)", Warning: true})
				continue
			}
			c.set(key, v.Content[j+1])
		}
	}
}

func (c *Config) set(key Key, v *yaml.Node) {
	if v.Kind != yaml.ScalarNode {
		c.Problems = append(c.Problems, Problem{File: c.Path, Line: v.Line, Key: key.Name, Message: "expected a single value"})
		return
	}
	raw := v.Value
	if v.Tag == "!!null" {
		raw = ""
	}
	if err := key.accept(raw); err != nil {
		c.Problems = append(c.Problems, Problem{File: c.Path, Line: v.Line, Key: key.Name, Message: err.Error()})
		return
	}
	c.values[key.Name] = Value{Key: key, Raw: raw, Source: FromFile, Line: v.Line}
}

// fill completes the values with the environment and the defaults.
func (c *Config) fill() {
	for _, k := range Keys {
		if env, ok := os.LookupEnv(k.Env()); ok {
			err := k.accept(env)
			if err == nil {
				c.values[k.Name] = Value{Key: k, Raw: env, Source: FromEnv}
				continue
			}
			c.Problems = append(c.Problems, Problem{File: "$" + k.Env(), Key: k.Name, Message: err.Error()})
		}
		if _, ok := c.values[k.Name]; !ok {
			c.values[k.Name] = Value{Key: k, Raw: k.Default, Source: FromDefault}
		}
	}
}

// Get returns the effective value of a key.
func (c *Config) Get(name string) Value {
	v, ok := c.values[name]
	if !ok {
		panic("config: unknown key " + name)
	}
	return v
}

// All returns every value in file order.
func (c *Config) All() []Value {
	out := make([]Value, 0, len(Keys))
	for _, k := range Keys {
		out = append(out, c.values[k.Name])
	}
	return out
}

// Str returns a text value.
func (c *Config) Str(name string) string { return c.Get(name).Raw }

// Int returns a number.
func (c *Config) Int(name string) int {
	n, _ := strconv.Atoi(c.Get(name).Raw)
	return n
}

// Bool returns a true/false value.
func (c *Config) Bool(name string) bool {
	b, _ := strconv.ParseBool(c.Get(name).Raw)
	return b
}

// Dur returns a duration.
func (c *Config) Dur(name string) time.Duration {
	d, _ := time.ParseDuration(c.Get(name).Raw)
	return d
}

// Language is the terminal language.
func (c *Config) Language() string { return c.Str("language") }

// StudentLanguage is the student web language for exams that set none:
// student_language, or the terminal language when that is empty.
func (c *Config) StudentLanguage() string {
	if l := c.Str("student_language"); l != "" {
		return l
	}
	return c.Language()
}
