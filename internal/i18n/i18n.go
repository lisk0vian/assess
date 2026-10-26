// Package i18n translates ax's interface: the terminal (TUI and CLI) and the
// student web app.
//
// Catalogs are YAML files named after their language (en.yaml, es.yaml).
// The built-in ones live in locales/ at the repository root and are embedded
// in the binary; more can be dropped into ~/.config/assess/locales/. Keys are
// nested maps flattened with dots ("editor.title"). A value is a string with
// {name} placeholders, or a map with "one" and "other" for plurals, chosen by
// the "n" argument. A key missing from a catalog falls back to English.
package i18n

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/lisk0vian/assess/locales"
)

// Fallback is the language of the reference catalog.
const Fallback = "en"

// Message is one translated string, or its plural forms.
type Message struct {
	Text  string
	One   string
	Other string
	Line  int // line in the catalog file, for check reports
}

func (m Message) plural() bool { return m.One != "" || m.Other != "" }

// Catalog is the messages of one language.
type Catalog struct {
	Lang     string
	Name     string // the language's own name, from the "language" key
	Source   string // file it was read from ("built-in" for embedded ones)
	Messages map[string]Message
}

var (
	mu       sync.RWMutex
	catalogs = map[string]*Catalog{}
	current  = Fallback
)

func init() {
	if err := loadEmbedded(); err != nil {
		panic(err)
	}
}

func loadEmbedded() error {
	entries, err := fs.ReadDir(locales.FS, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		lang, ok := strings.CutSuffix(e.Name(), ".yaml")
		if !ok {
			continue
		}
		data, err := fs.ReadFile(locales.FS, e.Name())
		if err != nil {
			return err
		}
		c, err := Parse(lang, data)
		if err != nil {
			return fmt.Errorf("locales/%s: %w", e.Name(), err)
		}
		c.Source = "built-in"
		catalogs[lang] = c
	}
	if catalogs[Fallback] == nil {
		return fmt.Errorf("locales: missing %s.yaml", Fallback)
	}
	return nil
}

var langPattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)

// ValidLang reports whether lang looks like a language code (es, pt-BR).
func ValidLang(lang string) bool { return langPattern.MatchString(lang) }

// LoadDir adds the catalogs found in dir (*.yaml). A user catalog for a
// built-in language completes and overrides it key by key. A missing dir
// is not an error; broken files are reported and skipped.
func LoadDir(dir string) []error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []error{err}
	}
	var errs []error
	mu.Lock()
	defer mu.Unlock()
	for _, e := range entries {
		lang, ok := strings.CutSuffix(e.Name(), ".yaml")
		if !ok || e.IsDir() || !ValidLang(lang) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c, err := Parse(lang, data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		c.Source = path
		if base := catalogs[lang]; base != nil {
			merged := &Catalog{Lang: lang, Name: base.Name, Source: base.Source + " + " + path, Messages: map[string]Message{}}
			for k, m := range base.Messages {
				merged.Messages[k] = m
			}
			for k, m := range c.Messages {
				merged.Messages[k] = m
			}
			if c.Name != "" {
				merged.Name = c.Name
			}
			c = merged
		}
		catalogs[lang] = c
	}
	return errs
}

// Parse reads a catalog file.
func Parse(lang string, data []byte) (*Catalog, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	c := &Catalog{Lang: lang, Messages: map[string]Message{}}
	if len(doc.Content) == 0 {
		return c, nil
	}
	if err := flatten(c, "", doc.Content[0]); err != nil {
		return nil, err
	}
	if m, ok := c.Messages["language"]; ok {
		c.Name = m.Text
	}
	return c, nil
}

func flatten(c *Catalog, prefix string, n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a map of keys", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		key := k.Value
		if prefix != "" {
			key = prefix + "." + key
		}
		switch v.Kind {
		case yaml.ScalarNode:
			c.Messages[key] = Message{Text: v.Value, Line: k.Line}
		case yaml.MappingNode:
			if isPlural(v) {
				m := Message{Line: k.Line}
				for j := 0; j+1 < len(v.Content); j += 2 {
					switch v.Content[j].Value {
					case "one":
						m.One = v.Content[j+1].Value
					case "other":
						m.Other = v.Content[j+1].Value
					}
				}
				c.Messages[key] = m
				continue
			}
			if err := flatten(c, key, v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("line %d: %s: expected text", k.Line, key)
		}
	}
	return nil
}

func isPlural(n *yaml.Node) bool {
	if len(n.Content) == 0 {
		return false
	}
	for i := 0; i < len(n.Content); i += 2 {
		if k := n.Content[i].Value; (k != "one" && k != "other") || n.Content[i+1].Kind != yaml.ScalarNode {
			return false
		}
	}
	return true
}

// SetLanguage switches the terminal language. An unknown language keeps
// English and returns an error.
func SetLanguage(lang string) error {
	mu.Lock()
	defer mu.Unlock()
	if catalogs[lang] == nil {
		current = Fallback
		return fmt.Errorf("no catalog for language %q (have: %s)", lang, strings.Join(langsLocked(), ", "))
	}
	current = lang
	return nil
}

// Language is the current terminal language.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Has reports whether a catalog exists for lang.
func Has(lang string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return catalogs[lang] != nil
}

// Languages lists the available catalogs, sorted by code.
func Languages() []*Catalog {
	mu.RLock()
	defer mu.RUnlock()
	var out []*Catalog
	for _, l := range langsLocked() {
		out = append(out, catalogs[l])
	}
	return out
}

func langsLocked() []string {
	var out []string
	for l := range catalogs {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// T translates key into the current language. args are name/value pairs
// that fill {name} placeholders; "n" also picks the plural form.
func T(key string, args ...any) string { return In(Language(), key, args...) }

// In translates key into lang.
func In(lang, key string, args ...any) string {
	mu.RLock()
	m, ok := lookup(lang, key)
	mu.RUnlock()
	if !ok {
		return key
	}
	text := m.Text
	if m.plural() {
		text = m.Other
		for i := 0; i+1 < len(args); i += 2 {
			if args[i] == "n" && isOne(args[i+1]) && m.One != "" {
				text = m.One
			}
		}
	}
	return fill(text, args)
}

func lookup(lang, key string) (Message, bool) {
	if c := catalogs[lang]; c != nil {
		if m, ok := c.Messages[key]; ok {
			return m, true
		}
	}
	m, ok := catalogs[Fallback].Messages[key]
	return m, ok
}

func isOne(v any) bool {
	switch n := v.(type) {
	case int:
		return n == 1
	case int64:
		return n == 1
	case float64:
		return n == 1
	}
	return false
}

func fill(text string, args []any) string {
	if !strings.Contains(text, "{") {
		return text
	}
	var b strings.Builder
	for {
		i := strings.IndexByte(text, '{')
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		j := strings.IndexByte(text[i:], '}')
		if j < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		name := text[i+1 : i+j]
		if v, ok := arg(args, name); ok {
			b.WriteString(v)
		} else {
			b.WriteString(text[i : i+j+1])
		}
		text = text[i+j+1:]
	}
}

func arg(args []any, name string) (string, bool) {
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == name {
			switch v := args[i+1].(type) {
			case string:
				return v, true
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64), true
			default:
				return fmt.Sprint(v), true
			}
		}
	}
	return "", false
}

// Section returns the messages under prefix (without it) for lang, with
// English filling the gaps. Plurals are returned as key.one / key.other.
// The web app gets its strings this way.
func Section(lang, prefix string) map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := map[string]string{}
	add := func(c *Catalog) {
		if c == nil {
			return
		}
		for k, m := range c.Messages {
			rest, ok := strings.CutPrefix(k, prefix+".")
			if !ok {
				continue
			}
			if m.plural() {
				out[rest+".one"], out[rest+".other"] = m.One, m.Other
			} else {
				out[rest] = m.Text
			}
		}
	}
	add(catalogs[Fallback])
	if lang != Fallback {
		add(catalogs[lang])
	}
	return out
}

// Problem is one finding of Check.
type Problem struct {
	Lang, Key, Message string
	Line               int
}

// Check compares every catalog with English: missing and extra keys, and
// placeholders that differ.
func Check() []Problem {
	mu.RLock()
	defer mu.RUnlock()
	ref := catalogs[Fallback]
	var out []Problem
	for _, lang := range langsLocked() {
		if lang == Fallback {
			continue
		}
		c := catalogs[lang]
		for _, k := range sortedKeys(ref.Messages) {
			m, ok := c.Messages[k]
			if !ok {
				out = append(out, Problem{Lang: lang, Key: k, Message: "missing (shown in English)"})
				continue
			}
			if want, got := placeholders(ref.Messages[k]), placeholders(m); !slices.Equal(want, got) {
				out = append(out, Problem{Lang: lang, Key: k, Line: m.Line, Message: fmt.Sprintf("placeholders %v, English has %v", got, want)})
			}
		}
		for _, k := range sortedKeys(c.Messages) {
			if _, ok := ref.Messages[k]; !ok {
				out = append(out, Problem{Lang: lang, Key: k, Line: c.Messages[k].Line, Message: "not in English (unused)"})
			}
		}
	}
	return out
}

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

func placeholders(m Message) []string {
	var out []string
	for _, s := range []string{m.Text, m.One, m.Other} {
		for _, p := range placeholderRe.FindAllStringSubmatch(s, -1) {
			if !slices.Contains(out, p[1]) {
				out = append(out, p[1])
			}
		}
	}
	slices.Sort(out)
	return out
}

func sortedKeys(m map[string]Message) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Keys lists the keys of the English catalog.
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	return sortedKeys(catalogs[Fallback].Messages)
}

// Template returns an empty catalog for a new language: the English file
// with every value to be translated, for contributors.
func Template() []byte {
	data, _ := fs.ReadFile(locales.FS, Fallback+".yaml")
	return bytes.Clone(data)
}
