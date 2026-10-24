package exam

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed scaffold
var scaffoldFS embed.FS

// ScaffoldOptions configures a new exam folder.
type ScaffoldOptions struct {
	ID       string // defaults to a slug of the folder name
	Title    string // defaults to a language-specific placeholder
	Language string // es (default) or en
}

// scaffoldText holds the translated sample content.
var scaffoldText = map[string]map[string]string{
	"es": {
		"title":        "Nuevo examen",
		"instructions": "Lee cada pregunta con atención. Las respuestas se guardan automáticamente.",
		"q1":           "¿Cuánto es $2 + 3$?",
		"q2":           "¿Cuáles de estos números son pares?",
		"q3":           "El número $7$ es primo.",
		"q4":           "Escribe el resultado de $6 \\times 7$.",
		"none":         "Ninguna de las anteriores",
	},
	"en": {
		"title":        "New exam",
		"instructions": "Read each question carefully. Answers are saved automatically.",
		"q1":           "What is $2 + 3$?",
		"q2":           "Which of these numbers are even?",
		"q3":           "The number $7$ is prime.",
		"q4":           "Write the result of $6 \\times 7$.",
		"none":         "None of the above",
	},
}

var (
	slugRe      = regexp.MustCompile(`[^a-z0-9_-]+`)
	foldAccents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e", "í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "ö", "o", "ú", "u", "ù", "u", "û", "u", "ü", "u", "ñ", "n", "ç", "c")
)

// Slug derives an exam id from a folder name ("Álgebra 01" -> "algebra-01").
// Callers should verify the result with ValidID.
func Slug(name string) string {
	s := slugRe.ReplaceAllString(foldAccents.Replace(strings.ToLower(name)), "-")
	s = strings.Trim(s, "-_")
	if len(s) > MaxIdentifierSize {
		s = strings.TrimRight(s[:MaxIdentifierSize], "-_")
	}
	return s
}

// Scaffold creates a new, valid exam folder at dir with one sample question
// of each type. It refuses to write into an existing non-empty folder.
func Scaffold(dir string, opts ScaffoldOptions) error {
	if opts.Language == "" {
		opts.Language = DefaultLanguage
	}
	text, ok := scaffoldText[opts.Language]
	if !ok {
		return fmt.Errorf("unsupported language %q (use es or en)", opts.Language)
	}
	if opts.ID == "" {
		opts.ID = Slug(filepath.Base(filepath.Clean(dir)))
	}
	if !ValidID(opts.ID) {
		return fmt.Errorf("cannot use %q as the exam id; pass one with --id (lowercase letters, digits, - and _)", opts.ID)
	}
	if opts.Title == "" {
		opts.Title = text["title"]
	}

	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", dir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	data := map[string]any{"ID": opts.ID, "Title": opts.Title, "Lang": opts.Language, "T": text}
	files := []struct {
		tmpl, out string
		mode      os.FileMode
	}{
		{"manifest.yaml.tmpl", ManifestFile, 0o644},
		{"q001.yaml.tmpl", QuestionsDir + "/q001.yaml", 0o644},
		{"q002.yaml.tmpl", QuestionsDir + "/q002.yaml", 0o644},
		{"q003.yaml.tmpl", QuestionsDir + "/q003.yaml", 0o644},
		{"q004.yaml.tmpl", QuestionsDir + "/q004.yaml", 0o644},
		{"answer-key.yaml.tmpl", AnswerKeyFile, 0o600},
	}
	tmpl, err := template.New("").Funcs(template.FuncMap{"yamlstr": yamlString}).
		ParseFS(scaffoldFS, "scaffold/*.tmpl")
	if err != nil {
		return err
	}
	for _, f := range files {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, f.tmpl, data); err != nil {
			return err
		}
		out := filepath.Join(dir, filepath.FromSlash(f.out))
		dirMode := os.FileMode(0o755)
		if strings.HasPrefix(f.out, PrivateDir+"/") {
			dirMode = 0o700
		}
		if err := os.MkdirAll(filepath.Dir(out), dirMode); err != nil {
			return err
		}
		if err := os.WriteFile(out, buf.Bytes(), f.mode); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(dir, AssetsDir), 0o755)
}

// yamlString quotes s as a YAML double-quoted scalar.
func yamlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}
