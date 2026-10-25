package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/docs"
	"github.com/lisk0vian/assess/internal/exam"
)

// The example in the authoring guide is a valid exam, file by file.
func TestAuthoringGuideExampleIsValid(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "revolucion-industrial")
	blocks := regexp.MustCompile("(?s)```yaml\n# file: ([^\n]+)\n(.*?)```").FindAllStringSubmatch(docs.Authoring, -1)
	if len(blocks) < 7 {
		t.Fatalf("expected the manifest, 5 questions and the key; found %d files", len(blocks))
	}
	for _, b := range blocks {
		path := filepath.Join(dir, filepath.FromSlash(strings.TrimSpace(b[1])))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(b[2]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, probs, err := exam.Load(dir)
	if err != nil || len(probs) > 0 {
		t.Fatalf("the guide's example does not validate: %v\n%v", err, probs)
	}
}

func TestFormatCommand(t *testing.T) {
	out, err := run(t, "format")
	if err != nil || !strings.Contains(out, "Writing an exam with an AI assistant") || !strings.Contains(out, "# Exam format `assess/v1`") {
		t.Fatalf("format: %v\n%.200s", err, out)
	}
	out, _ = run(t, "format", "--guide")
	if strings.Contains(out, "# Exam format `assess/v1`") {
		t.Fatal("--guide should print only the guide")
	}
}
