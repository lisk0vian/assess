package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

func endSession(t *testing.T, dbPath, id string) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := session.NewService(db).End(ctx, id); err != nil {
		t.Fatal(err)
	}
}

// Two classes that sat the same exam, side by side, with the warning.
func TestCompareClasses(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t, dir)
	d := []string{"--data-dir", dir}
	ok(t, "", append([]string{"exam", "import", example}, d...)...)
	for _, c := range []string{"3B", "3C", "4A"} {
		ok(t, "", append([]string{"class", "new", c}, d...)...)
	}
	endSession(t, db, sitExam(t, db, "3B", []string{"A-1 Ana", "A-2 Bea", "A-3 Carla"}))
	if _, err := run(t, append([]string{"exam", "compare", "algebra-01"}, d...)...); err == nil || !strings.Contains(err.Error(), "two classes") {
		t.Fatalf("one class is not a comparison: %v", err)
	}
	endSession(t, db, sitExam(t, db, "3C", []string{"B-1 Dani", "B-2 Eva"}))

	out := ok(t, "", append([]string{"exam", "compare", "algebra-01"}, d...)...)
	for _, want := range []string{"in 2 classes", "3B", "3C", "about the same on every question", "not comparable by themselves"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if _, err := run(t, append([]string{"exam", "compare", "algebra-01", "3B", "4A"}, d...)...); err == nil || !strings.Contains(err.Error(), "4A") {
		t.Fatalf("a class that never sat it is named: %v", err)
	}
	if _, err := run(t, append([]string{"exam", "compare", "nada"}, d...)...); err == nil {
		t.Fatal("an unknown exam is refused")
	}
}
