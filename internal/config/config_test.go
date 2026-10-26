package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateLoadsAsDefaults(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := Create(path, lang); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Problems) > 0 {
			t.Fatalf("%s template has problems: %v", lang, c.Problems)
		}
		for _, v := range c.All() {
			if v.Source != FromDefault {
				t.Errorf("%s: %s comes from %s, want default", lang, v.Key.Name, v.Source)
			}
		}
		data, _ := os.ReadFile(path)
		for _, k := range Keys {
			if !strings.Contains(string(data), "# "+k.Leaf()+":") {
				t.Errorf("%s template lacks %s", lang, k.Name)
			}
		}
		if strings.Contains(string(data), "config.key.") {
			t.Errorf("%s template has untranslated help:\n%s", lang, data)
		}
	}
}

func TestSetKeepsTheRestOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Create(path, "en"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	for _, kv := range [][2]string{{"server.port", "8080"}, {"language", "es"}, {"results.separator", ";"}, {"new_exam.instructions", "Good luck: no phones."}} {
		if err := Set(path, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	c, _ := Load(path)
	if len(c.Problems) > 0 {
		t.Fatal(c.Problems)
	}
	if c.Int("server.port") != 8080 || c.Language() != "es" || c.Str("results.separator") != ";" || c.Str("new_exam.instructions") != "Good luck: no phones." {
		t.Fatalf("values not set: %+v", c.All())
	}
	if v := c.Get("server.port"); v.Source != FromFile || v.Line == 0 {
		t.Fatalf("port source %+v", v)
	}
	after, _ := os.ReadFile(path)
	// Only the four lines changed.
	b, a := strings.Split(string(before), "\n"), strings.Split(string(after), "\n")
	if len(a) != len(b) {
		t.Fatalf("lines %d → %d:\n%s", len(b), len(a), after)
	}
	diff := 0
	for i := range a {
		if a[i] != b[i] {
			diff++
		}
	}
	if diff != 4 {
		t.Fatalf("%d lines changed:\n%s", diff, after)
	}

	// Unset puts the comment back.
	if err := Unset(path, "server.port"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(path)
	if v := c.Get("server.port"); v.Source != FromDefault || v.Raw != "27337" {
		t.Fatalf("after unset: %+v", v)
	}

	if err := Set(path, "server.port", "70000"); err == nil {
		t.Fatal("an out-of-range port must be refused")
	}
	if err := Set(path, "server.nope", "1"); err == nil {
		t.Fatal("an unknown key must be refused")
	}
}

func TestSetWithoutTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("# mine\nresults:\n  format: json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "results.bom", "false"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "panel.lock_minutes", "0"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "editor", "code -w"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# mine\neditor: code -w\nresults:\n  format: json\n  bom: false\n\npanel:\n  lock_minutes: 0\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestProblemsHaveLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	src := "language: en\nserver:\n  port: abc\n  colour: blue\nresults:\n  format: xml\nwhat: 1\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ := Load(path)
	got := map[string]Problem{}
	for _, p := range c.Problems {
		got[p.Key] = p
	}
	if p := got["server.port"]; p.Line != 3 || p.Warning {
		t.Errorf("port: %+v", p)
	}
	if p := got["server.colour"]; p.Line != 4 || !p.Warning {
		t.Errorf("unknown key: %+v", p)
	}
	if p := got["results.format"]; p.Line != 6 || !strings.Contains(p.Message, "csv, json") {
		t.Errorf("format: %+v", p)
	}
	if p := got["what"]; !p.Warning {
		t.Errorf("unknown top-level: %+v", p)
	}
	if !c.HasErrors() || c.Int("server.port") != 27337 {
		t.Fatal("invalid values fall back to the default and count as errors")
	}
	if !strings.HasPrefix(got["server.port"].Error(), path+":3: error: server.port: ") {
		t.Errorf("message: %s", got["server.port"].Error())
	}

	if err := os.WriteFile(path, []byte("server:\n  port: [1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(path)
	if !c.HasErrors() || c.Problems[0].Line == 0 {
		t.Fatalf("broken YAML: %+v", c.Problems)
	}
}

func TestEnvironmentWinsOverFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AX_SERVER_PORT", "9090")
	c, _ := Load(path)
	if v := c.Get("server.port"); v.Raw != "9090" || v.Source != FromEnv {
		t.Fatalf("got %+v", v)
	}
	if c.StudentLanguage() != "en" {
		t.Fatal("student language follows the terminal language")
	}
	t.Setenv("AX_STUDENT_LANGUAGE", "es")
	if c, _ := Load(path); c.StudentLanguage() != "es" {
		t.Fatal("student_language overrides")
	}
	t.Setenv("AX_LANGUAGE", "xx")
	if c, _ := Load(path); !c.HasErrors() || c.Language() != "en" {
		t.Fatal("a language without a catalog is an error")
	}
}

func TestPath(t *testing.T) {
	t.Setenv(EnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join("x", "cfg"))
	p, _ := Path("")
	if !strings.HasSuffix(p, filepath.Join("x", "cfg", "assess", "config.yaml")) {
		t.Fatalf("XDG: %s", p)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	if p, _ := Path(""); p != filepath.Join(home, ".config", "assess", "config.yaml") {
		t.Fatalf("home: %s", p)
	}
	t.Setenv(EnvVar, filepath.Join(home, "other.yaml"))
	if p, _ := Path(""); p != filepath.Join(home, "other.yaml") {
		t.Fatalf("env: %s", p)
	}
	if p, _ := Path(filepath.Join(home, "flag.yaml")); p != filepath.Join(home, "flag.yaml") {
		t.Fatalf("flag: %s", p)
	}
}
