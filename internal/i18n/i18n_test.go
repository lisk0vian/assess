package i18n_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/i18n"
)

// Every built-in translation is complete and keeps the placeholders.
func TestBuiltInCatalogsAreComplete(t *testing.T) {
	for _, p := range i18n.Check() {
		t.Errorf("%s:%d: %s: %s", p.Lang, p.Line, p.Key, p.Message)
	}
}

// Every key the code asks for exists in English.
func TestUsedKeysExist(t *testing.T) {
	keys := i18n.Keys()
	have := func(k string) bool { _, found := slices.BinarySearch(keys, k); return found }
	goKey := regexp.MustCompile(`\bT\("([a-z0-9_.]+)"`)
	webKey := regexp.MustCompile(`\bt\("([a-z0-9_]+)"`)
	check := func(root string, re *regexp.Regexp, prefix string, exts ...string) int {
		n := 0
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !slices.Contains(exts, filepath.Ext(p)) || strings.HasSuffix(p, "_test.go") {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				k := prefix + m[1]
				if strings.HasSuffix(k, ".") {
					continue // a prefix completed at run time; see TestDynamicKeys
				}
				n++
				if !have(k) && !have(k+".one") {
					t.Errorf("%s: key %q is not in locales/en.yaml", p, k)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := check(filepath.Join("..", ".."), goKey, "", ".go"); n < 300 {
		t.Fatalf("only %d keys found in the Go code; is the pattern still right?", n)
	}
	if n := check(filepath.Join("..", "..", "web", "src"), webKey, "web.", ".ts", ".tsx"); n < 50 {
		t.Fatalf("only %d keys found in the web app", n)
	}
}

// Keys built at run time from a prefix and a value.
func TestDynamicKeys(t *testing.T) {
	sets := map[string][]string{
		"state.":           {"lobby", "running", "ended"},
		"astate.":          {"active", "submitted", "annulled", "withdrawn", "absent"},
		"pick.kind.":       {"student", "class", "exam", "session"},
		"find.type.":       {"student", "class", "exam", "session"},
		"vault.source.":    {"flag", "folder", "setting", "default"},
		"explore.empty.":   {"exams", "classes", "students", "sessions"},
		"explore.key.new.": {"exams", "classes", "students"},
		"explore.act.": {"back", "history", "edit", "rename", "enroll", "unenroll", "merge", "forget", "members", "import",
			"add_student", "gradebook", "export", "rename_class", "delete", "start", "edit_exam", "sessions", "export_exam",
			"report", "results"},
		"monitor.state.":            {"active", "submitted", "annulled", "withdrawn"},
		"qtype.":                    {"single", "multiple", "truefalse", "short"},
		"penalty.":                  {"strike", "void", "points"},
		"severity.":                 {"info", "warn", "high"},
		"profile.":                  {"standard", "strict", "off"},
		"form.profile.":             {"standard", "strict", "off"},
		"form.visibility.":          {"none", "score", "review"},
		"editor.visibility.":        {"none", "score", "review"},
		"wizard.review.visibility.": {"none", "score", "review"},
		"wizard.profile_text.":      {"standard", "strict", "off"},
		"editor.shuffle.":           {"true_true", "true_false", "false_true", "false_false"},
		"wizard.review.shuffle.":    {"true_true", "true_false", "false_true", "false_false"},
		"cli.config.kind.":          {"text", "number", "bool", "duration", "choice"},
		"signal.": {"hidden", "blur", "copy", "screenshot", "paste_input", "burst", "fullscreen_exit", "untrusted", "webdriver",
			"dom_injection", "resize", "second_tab", "telemetry", "telemetry_gap", "frozen_pointer"},
	}
	for prefix, values := range sets {
		for _, v := range values {
			if got := i18n.In("en", prefix+v); got == prefix+v {
				t.Errorf("missing %s%s", prefix, v)
			}
		}
	}
}

func TestPluralsAndPlaceholders(t *testing.T) {
	if got := i18n.In("en", "report.n.students", "n", 1); got != "1 student" {
		t.Fatalf("one: %q", got)
	}
	if got := i18n.In("es", "report.n.students", "n", 3); got != "3 alumnos" {
		t.Fatalf("other: %q", got)
	}
	if got := i18n.In("xx", "editor.done"); got != "Done." {
		t.Fatalf("unknown languages fall back to English: %q", got)
	}
	if got := i18n.In("es", "no.such.key"); got != "no.such.key" {
		t.Fatalf("a missing key shows itself: %q", got)
	}
}

func TestUserCatalog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pt.yaml"), []byte("language: Português\neditor:\n  done: Feito.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "es.yaml"), []byte("editor:\n  done: ¡Listo!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := i18n.LoadDir(dir); len(errs) > 0 {
		t.Fatal(errs)
	}
	if !i18n.Has("pt") || i18n.In("pt", "editor.done") != "Feito." || i18n.In("pt", "editor.saved") != "Saved." {
		t.Fatal("a new language loads and falls back to English")
	}
	if i18n.In("es", "editor.done") != "¡Listo!" || i18n.In("es", "editor.saved") != "Guardado." {
		t.Fatal("a user catalog completes a built-in one key by key")
	}
	if section := i18n.Section("pt", "web"); section["join_title"] != "Join the exam" {
		t.Fatalf("web strings fall back to English: %q", section["join_title"])
	}
}
