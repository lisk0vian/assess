package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

func TestResultsExport(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, testDB(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := exam.Publish(filepath.Join("..", "..", "examples", "algebra-01"))
	if err != nil {
		t.Fatal(err)
	}
	svc := session.NewService(db)
	sess, _ := svc.CreateSession(ctx, pub, session.CreateOptions{})
	ana, _, _ := svc.Join(ctx, sess.ID, "A1", "Ana Pérez")
	_, _, _ = svc.Join(ctx, sess.ID, "B2", "Bruno")
	_, _ = svc.Start(ctx, sess.ID)
	f := false
	for _, sl := range ana.Layout {
		if sl.QuestionID == "q003" {
			_, _ = svc.SaveAnswer(ctx, ana.ID, "q003", 1, grading.Response{Value: &f})
		}
	}
	if _, err := svc.Submit(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out, err := run(t, "results", "--data-dir", dir, "--no-bom")
	if err != nil {
		t.Fatalf("results: %v\n%s", err, out)
	}
	recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0][0] != "student_id" || len(recs[0]) != 9+7 {
		t.Fatalf("unexpected CSV:\n%s", out)
	}
	ana1 := recs[1]
	if ana1[1] != "Ana Pérez" || ana1[2] != "submitted" || ana1[3] != "1" || ana1[4] != "9" || ana1[7] != "manual" {
		t.Fatalf("unexpected row for Ana: %v", ana1)
	}
	if recs[2][2] != "active" || recs[2][3] != "" {
		t.Fatalf("unexpected row for Bruno: %v", recs[2])
	}

	out, err = run(t, "results", "--data-dir", dir, "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	var res session.Results
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.ExamID != "algebra-01" || len(res.Rows) != 2 {
		t.Fatalf("json export: %v %+v", err, res)
	}

	bom, _ := run(t, "results", "--data-dir", dir, "--sep", ";")
	if !strings.HasPrefix(bom, "\xef\xbb\xbfstudent_id;name;") {
		t.Fatalf("expected BOM and ; separator, got %q", bom[:40])
	}
}
