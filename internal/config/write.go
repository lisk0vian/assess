package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/lisk0vian/assess/internal/i18n"
)

// Template is the file written on the first run: every setting commented
// out with its default and a short explanation, in lang.
func Template(lang string) []byte {
	var b strings.Builder
	for _, l := range strings.Split(i18n.In(lang, "config.file_header"), "\n") {
		b.WriteString(strings.TrimRight("# "+l, " ") + "\n")
	}
	for _, g := range Groups() {
		indent := ""
		if g != "" {
			b.WriteString("\n" + comment("", i18n.In(lang, "config.group."+g)))
			b.WriteString(g + ":\n")
			indent = "  "
		}
		first := true
		for _, k := range Keys {
			if k.Group() != g {
				continue
			}
			if !first || g == "" {
				b.WriteString("\n")
			}
			first = false
			b.WriteString(comment(indent, Help(lang, k)))
			b.WriteString(indent + "# " + k.Leaf() + ": " + quote(k, k.Default) + "\n")
		}
	}
	return []byte(b.String())
}

// Help is the explanation of a key, with its accepted values.
func Help(lang string, k Key) string {
	h := i18n.In(lang, "config.key."+k.Name)
	if k.Kind == Choice {
		h += " (" + strings.Join(k.Choices, " | ") + ")"
	}
	return h
}

func comment(indent, text string) string {
	var b strings.Builder
	for _, l := range strings.Split(text, "\n") {
		b.WriteString(strings.TrimRight(indent+"# "+l, " ") + "\n")
	}
	return b.String()
}

// quote writes v as YAML would read it back as the same text.
func quote(k Key, v string) string {
	if k.Kind != String && v != "" {
		return v
	}
	out, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%q", v)
	}
	return strings.TrimSpace(string(out))
}

// Create writes the template at path unless a file is already there.
func Create(path, lang string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, Template(lang))
}

// Set writes name: value into the file at path, keeping everything else
// as it is: comments, order, other settings. A commented-out line for the
// key is turned into the setting.
func Set(path, name, value string) error {
	k, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown setting %q (see `ax config`)", name)
	}
	if err := k.accept(value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return edit(path, k, k.Leaf()+": "+quote(k, value))
}

// Unset comments the key out again, so its default applies.
func Unset(path, name string) error {
	k, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown setting %q (see `ax config`)", name)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return edit(path, k, "# "+k.Leaf()+": "+quote(k, k.Default))
}

// edit replaces the key's line (active or commented) with line, or adds it
// at the end of its group.
func edit(path string, k Key, line string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	leaf := regexp.QuoteMeta(k.Leaf())
	if k.Group() == "" {
		active := regexp.MustCompile(`^` + leaf + `\s*:`)
		commented := regexp.MustCompile(`^#\s?` + leaf + `\s*:`)
		lines = replaceOrInsert(lines, 0, len(lines), active, commented, line, firstGroup(lines))
	} else {
		start := findGroup(lines, k.Group())
		if start < 0 {
			lines = append(lines, "", k.Group()+":")
			start = len(lines) - 1
		}
		end := topEnd(lines, start+1)
		active := regexp.MustCompile(`^\s+` + leaf + `\s*:`)
		commented := regexp.MustCompile(`^\s+#\s?` + leaf + `\s*:`)
		// Insert after the last indented line of the group.
		at := start + 1
		for i := start + 1; i < end; i++ {
			if strings.HasPrefix(lines[i], " ") {
				at = i + 1
			}
		}
		lines = replaceOrInsert(lines, start+1, end, active, commented, "  "+line, at)
	}
	return writeAtomic(path, []byte(strings.Join(lines, "\n")+"\n"))
}

func replaceOrInsert(lines []string, from, to int, active, commented *regexp.Regexp, line string, at int) []string {
	for i := from; i < to; i++ {
		if active.MatchString(lines[i]) {
			lines[i] = line
			return lines
		}
	}
	// The last match: a help comment above could mention the key too.
	for i := to - 1; i >= from; i-- {
		if commented.MatchString(lines[i]) {
			lines[i] = line
			return lines
		}
	}
	out := append([]string{}, lines[:at]...)
	out = append(out, line)
	return append(out, lines[at:]...)
}

// findGroup returns the line of "group:", uncommenting it if needed.
func findGroup(lines []string, g string) int {
	active := regexp.MustCompile(`^` + regexp.QuoteMeta(g) + `\s*:\s*(#.*)?$`)
	commented := regexp.MustCompile(`^#\s?` + regexp.QuoteMeta(g) + `\s*:\s*$`)
	for i, l := range lines {
		if active.MatchString(l) {
			return i
		}
	}
	for i, l := range lines {
		if commented.MatchString(l) {
			lines[i] = g + ":"
			return i
		}
	}
	return -1
}

// topEnd returns the index of the next group line at or after from (or
// len(lines)): the end of a block.
func topEnd(lines []string, from int) int {
	for i := from; i < len(lines); i++ {
		l := lines[i]
		if l != "" && l[0] != ' ' && l[0] != '#' && strings.HasSuffix(strings.TrimSpace(strings.SplitN(l, "#", 2)[0]), ":") {
			return i
		}
	}
	return len(lines)
}

// firstGroup is where a new top-level key goes: before the comments of
// the first group, or at the end.
func firstGroup(lines []string) int {
	end := topEnd(lines, 0)
	if end == len(lines) {
		return end
	}
	at := end
	for at > 0 && strings.HasPrefix(lines[at-1], "#") {
		at--
	}
	if at == 0 {
		return end // the comments are the file's header, not the group's
	}
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	return at
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
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
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
