package exam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleDir = "../../examples/algebra-01"

func TestExampleIsValid(t *testing.T) {
	ex, probs, err := Load(exampleDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 0 {
		t.Fatalf("example has problems:\n%s", dump(probs))
	}
	got := ex.Stats()
	want := Stats{TotalQuestions: 7, QuestionsPerAttempt: 6, PointsPerAttempt: 9}
	if got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
	if !ex.Manifest.ShuffleQuestions() || ex.Manifest.Profile() != "standard" || ex.Manifest.Lang() != "es" {
		t.Fatalf("unexpected manifest defaults: %+v", ex.Manifest)
	}
	if d := ex.Questions["q007"].TimeLimit.D().String(); d != "1m30s" {
		t.Fatalf("q007 time_limit = %s", d)
	}
}

func TestScaffoldIsValid(t *testing.T) {
	for _, lang := range []string{"es", "en"} {
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "Mi Examen")
			if err := Scaffold(dir, ScaffoldOptions{Language: lang, Title: `Quotes "and" \slashes`}); err != nil {
				t.Fatal(err)
			}
			ex, probs, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(probs) != 0 {
				t.Fatalf("scaffold has problems:\n%s", dump(probs))
			}
			if ex.Manifest.ID != "mi-examen" || ex.Manifest.Title != `Quotes "and" \slashes` || ex.Manifest.Lang() != lang {
				t.Fatalf("unexpected manifest: %+v", ex.Manifest)
			}
			if err := Scaffold(dir, ScaffoldOptions{}); err == nil {
				t.Fatal("scaffold into a non-empty folder succeeded")
			}
		})
	}
}

func TestScaffoldRejectsBadInput(t *testing.T) {
	if err := Scaffold(filepath.Join(t.TempDir(), "###"), ScaffoldOptions{}); err == nil {
		t.Fatal("expected an error for an id that cannot be derived")
	}
	if err := Scaffold(filepath.Join(t.TempDir(), "x"), ScaffoldOptions{Language: "fr"}); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Álgebra Básica":  "algebra-basica",
		"algebra-01":      "algebra-01",
		"  Física_II  ":   "fisica_ii",
		"Examen (final)!": "examen-final",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadMissingFolder(t *testing.T) {
	if _, _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error")
	}
}

// edit describes a change applied to a copy of the example exam.
type edit struct {
	file string
	old  string // replaced by new; empty means append new
	new  string
}

func TestInvalidExams(t *testing.T) {
	cases := []struct {
		name  string
		edits []edit
		rm    []string // files to delete
		add   map[string]string
		file  string
		field string
		msg   string
		line  string // when set, the problem must point at the line containing this text
		sev   Severity
	}{
		{name: "unknown manifest field", edits: []edit{{file: ManifestFile, new: "colour: red\n"}},
			file: ManifestFile, msg: `unknown field "colour"`, line: "colour: red"},
		{name: "bad duration", edits: []edit{{ManifestFile, "duration: 45m", "duration: 45 minutes"}},
			file: ManifestFile, msg: `invalid duration "45 minutes"`, line: "duration:"},
		{name: "syntax error", edits: []edit{{ManifestFile, `title: "Álgebra 1 — Ecuaciones lineales"`, "title: [unclosed"}},
			file: ManifestFile, msg: "did not find expected"},
		{name: "type mismatch", edits: []edit{{ManifestFile, "pick: 2", "pick: two"}},
			file: ManifestFile, msg: `expected an integer, got "two"`, line: "pick: two"},
		{name: "unsupported format", edits: []edit{{ManifestFile, "format: assess/v1", "format: assess/v9"}},
			file: ManifestFile, field: "format", msg: "unsupported format"},
		{name: "invalid enum", edits: []edit{{ManifestFile, "result_visibility: score", "result_visibility: always"}},
			file: ManifestFile, field: "result_visibility", msg: "use one of: none, score, review"},
		{name: "invalid accent", edits: []edit{{ManifestFile, `accent: "#4f46e5"`, `accent: indigo`}},
			file: ManifestFile, field: "branding.accent", msg: "invalid color"},
		{name: "unknown question", edits: []edit{{ManifestFile, "question: q006", "question: q099"}},
			file: ManifestFile, field: "items[4].question", msg: `unknown question "q099"`, line: "question: q099"},
		{name: "unused question", edits: []edit{{ManifestFile, "question: q006", "question: q099"}},
			file: "questions/q006.yaml", msg: "not used by any item", sev: SeverityWarning},
		{name: "question used twice", edits: []edit{{ManifestFile, "question: q002", "question: q001"}},
			file: ManifestFile, field: "items[1].question", msg: `"q001" is already used at items[0].question`},
		{name: "pool pick too large", edits: []edit{{ManifestFile, "pick: 2", "pick: 3"}},
			file: ManifestFile, field: "items[3].pool.pick", msg: "between 1 and 2", line: "pick: 3"},
		{name: "item with question and pool", edits: []edit{{ManifestFile, "  - pool:\n", "  - question: q006\n    pool:\n"}},
			file: ManifestFile, field: "items[3]", msg: "not both"},
		{name: "pool points differ", edits: []edit{{AnswerKeyFile, "option: a\n    points: 2", "option: a\n    points: 1"}},
			file: ManifestFile, field: "items[3].pool.from", msg: "same points (q004: 2, q005: 1, q007: 2)"},
		{name: "id does not match file", edits: []edit{{"questions/q003.yaml", "id: q003", "id: q003b"}},
			file: "questions/q003.yaml", field: "id", msg: "must match the file name", line: "id: q003b"},
		{name: "duplicate option id", edits: []edit{{"questions/q001.yaml", "  - id: b", "  - id: a"}},
			file: "questions/q001.yaml", field: "options[1].id", msg: `duplicate option id "a"`},
		{name: "options on truefalse", edits: []edit{{file: "questions/q003.yaml", new: "options:\n  - id: a\n    text: x\n"}},
			file: "questions/q003.yaml", field: "options", msg: "only single and multiple"},
		{name: "short without input", edits: []edit{{"questions/q004.yaml", "input: number\n", ""}},
			file: "questions/q004.yaml", field: "input", msg: "required for short questions"},
		{name: "reserved type", edits: []edit{{"questions/q003.yaml", "type: truefalse", "type: essay"}},
			file: "questions/q003.yaml", field: "type", msg: "reserved for a future version"},
		{name: "time limit longer than exam", edits: []edit{{"questions/q007.yaml", "time_limit: 90s", "time_limit: 2h"}},
			file: "questions/q007.yaml", field: "time_limit", msg: "longer than the exam duration"},
		{name: "external image", edits: []edit{{file: "questions/q001.yaml", old: "Resuelve la ecuación", new: "![x](https://example.com/a.png) Resuelve"}},
			file: "questions/q001.yaml", field: "prompt", msg: "external image"},
		{name: "missing asset", edits: []edit{{file: "questions/q001.yaml", old: "Resuelve la ecuación", new: "![x](assets/nope.png) Resuelve"}},
			file: "questions/q001.yaml", field: "prompt", msg: `asset "assets/nope.png" not found`},
		{name: "asset outside assets", edits: []edit{{file: "questions/q001.yaml", old: "Resuelve la ecuación", new: "![x](assets/../manifest.yaml) Resuelve"}},
			file: "questions/q001.yaml", field: "prompt", msg: "must point to a file under assets/"},
		{name: "yml extension", add: map[string]string{"questions/q008.yml": "id: q008\n"},
			file: "questions/q008.yml", msg: "use the .yaml extension"},
		{name: "key missing", rm: []string{AnswerKeyFile},
			file: AnswerKeyFile, msg: "file not found"},
		{name: "key missing entry", edits: []edit{{AnswerKeyFile, "  q003:\n    value: false\n    points: 1\n", ""}},
			file: AnswerKeyFile, field: "answers", msg: `missing entry for question "q003"`},
		{name: "key field for wrong type", edits: []edit{{AnswerKeyFile, "option: b\n", "option: b\n    value: true\n"}},
			file: AnswerKeyFile, field: "answers.q001.value", msg: "does not apply to single questions"},
		{name: "key unknown option", edits: []edit{{AnswerKeyFile, "option: b", "option: z"}},
			file: AnswerKeyFile, field: "answers.q001.option", msg: `unknown option "z"`, line: "option: z"},
		{name: "key exam mismatch", edits: []edit{{AnswerKeyFile, "exam: algebra-01", "exam: other"}},
			file: AnswerKeyFile, field: "exam", msg: "does not match the manifest id"},
		{name: "key bad normalizer", edits: []edit{{AnswerKeyFile, "strip_accents", "strip_emoji"}},
			file: AnswerKeyFile, field: "answers.q006.normalize[3]", msg: `invalid value "strip_emoji"`},
		{name: "key zero points", edits: []edit{{AnswerKeyFile, "option: b\n    points: 1", "option: b\n    points: 0"}},
			file: AnswerKeyFile, field: "answers.q001.points", msg: "greater than 0"},
		{name: "key unknown question", edits: []edit{{file: AnswerKeyFile, new: "  q099:\n    option: a\n    points: 1\n"}},
			file: AnswerKeyFile, field: "answers.q099", msg: `unknown question "q099"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyExample(t)
			for _, e := range tc.edits {
				applyEdit(t, dir, e)
			}
			for _, f := range tc.rm {
				if err := os.Remove(filepath.Join(dir, f)); err != nil {
					t.Fatal(err)
				}
			}
			for f, content := range tc.add {
				writeFile(t, filepath.Join(dir, f), content)
			}

			_, probs, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			sev := tc.sev
			if sev == "" {
				sev = SeverityError
			}
			wantLine := 0
			if tc.line != "" {
				wantLine = lineContaining(t, filepath.Join(dir, tc.file), tc.line)
			}
			for _, p := range probs {
				if p.File == tc.file && p.Severity == sev && strings.Contains(p.Message, tc.msg) &&
					(tc.field == "" || p.Field == tc.field) && (wantLine == 0 || p.Line == wantLine) {
					return
				}
			}
			t.Fatalf("no %s in %s (field %q, line %d) containing %q; got:\n%s",
				sev, tc.file, tc.field, wantLine, tc.msg, dump(probs))
		})
	}
}

func copyExample(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "algebra-01")
	err := filepath.WalkDir(exampleDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(exampleDir, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func applyEdit(t *testing.T, dir string, e edit) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(e.file))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if e.old == "" {
		s += e.new
	} else {
		if !strings.Contains(s, e.old) {
			t.Fatalf("%s does not contain %q", e.file, e.old)
		}
		s = strings.Replace(s, e.old, e.new, 1)
	}
	writeFile(t, path, s)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lineContaining(t *testing.T, path, text string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, text) {
			return i + 1
		}
	}
	t.Fatalf("%s has no line containing %q", path, text)
	return 0
}

func dump(ps Problems) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString("  " + string(p.Severity) + " " + p.String() + "\n")
	}
	return b.String()
}
