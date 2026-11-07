package cli

import (
	"context"
	"testing"

	"github.com/lisk0vian/assess/internal/session"
)

// A correction from the report regrades the session and rewrites its archive.
func TestReportFixerCorrectsTheVault(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t, dir)
	ok(t, "", "class", "create", "3B", "--data-dir", dir)
	id := sitExam(t, db, "3B", []string{"A-12 Ana", "B-07 Luis"})
	ctx := context.Background()
	if err := endOpenSession(ctx, dir); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetContext(ctx)
	fix := reportFixer(cmd, dir, id)
	if fix == nil {
		t.Fatal("an ended session can be corrected")
	}
	rep, err := fix.Apply(session.Correction{QuestionID: "q001", Kind: session.CorrectVoid})
	if err != nil {
		t.Fatal(err)
	}
	if got := fix.Corrections(); len(got) != 1 || rep == nil {
		t.Fatalf("corrections %+v", got)
	}
	if _, err := fix.Undo(fix.Corrections()[0].ID); err != nil || len(fix.Corrections()) != 0 {
		t.Fatalf("undo: %v", err)
	}
}
