package exam

import (
	"os"
	"path/filepath"
	"testing"
)

// The examples are documentation people run with ax: every exam under
// examples/ must validate cleanly, and every one under examples/invalid/
// must fail.
func TestExamples(t *testing.T) {
	check := func(dir string, wantErrors bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == "invalid" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			t.Run(filepath.ToSlash(path), func(t *testing.T) {
				_, probs, err := Load(path)
				if err != nil && !wantErrors {
					t.Fatal(err)
				}
				switch {
				case wantErrors && !probs.HasErrors() && err == nil:
					t.Fatal("an invalid example validates")
				case !wantErrors && len(probs) > 0:
					t.Fatalf("problems: %v", probs)
				}
			})
		}
	}
	check("../../examples", false)
	check("../../examples/invalid", true)
}
