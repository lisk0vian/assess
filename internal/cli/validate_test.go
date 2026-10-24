package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestValidateBrokenGolden(t *testing.T) {
	out, err := run(t, "validate", "testdata/broken")
	if !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent", err)
	}
	golden := filepath.Join("testdata", "validate_broken.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Fatalf("output mismatch (run with -update to accept)\n--- got\n%s--- want\n%s", out, want)
	}
}

func TestValidateExampleAndJSON(t *testing.T) {
	out, err := run(t, "validate", "../../examples/algebra-01")
	if err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "ok  algebra-01: 7 questions, 6 per attempt, 9 points per attempt") {
		t.Fatalf("unexpected output %q", out)
	}

	out, err = run(t, "validate", "--json", "../../examples/algebra-01", "testdata/broken")
	if !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent", err)
	}
	var reports []validateReport
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(reports) != 2 || !reports[0].Valid || reports[1].Valid || reports[1].Errors != 10 {
		t.Fatalf("unexpected reports: %+v", reports)
	}
}

func TestValidateStrict(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quiz")
	if _, err := run(t, "new", dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "validate", dir); err != nil {
		t.Fatalf("warnings must not fail without --strict: %v\n%s", err, out)
	}
	if _, err := run(t, "validate", "--strict", dir); !errors.Is(err, errSilent) {
		t.Fatalf("--strict: err = %v, want errSilent", err)
	}
}
