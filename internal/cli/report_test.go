package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/grading"
	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
)

func TestReport(t *testing.T) {
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
	t.Cleanup(func() { db.Close() })
	svc := session.NewService(db)
	sess, _ := svc.CreateSession(ctx, pub, session.CreateOptions{})
	var atts []*session.Attempt
	for i := range 8 {
		a, _, err := svc.Join(ctx, sess.ID, fmt.Sprintf("S%d", i), fmt.Sprintf("Alumno %d", i))
		if err != nil {
			t.Fatal(err)
		}
		atts = append(atts, a)
	}
	if _, err := svc.Start(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	f := false
	for i, a := range atts {
		seq := int64(0)
		save := func(q string, r grading.Response) {
			if !slices.ContainsFunc(a.Layout, func(sl exam.Slot) bool { return sl.QuestionID == q }) {
				return // pools: not every student gets every question
			}
			seq++
			if _, err := svc.SaveAnswer(ctx, a.ID, q, seq, r); err != nil {
				t.Fatal(err)
			}
		}
		save("q001", grading.Response{Option: "b"})
		save("q003", grading.Response{Value: &f})
		save("q004", grading.Response{Text: fmt.Sprint(4 + i%3)})
		save("q007", grading.Response{Text: fmt.Sprint(10 + i)})
		if i < 4 {
			save("q005", grading.Response{Option: "a"})
		}
	}
	db.Close()

	// While the session runs there is no report.
	if _, err := run(t, "report", "--data-dir", dir); err == nil || !strings.Contains(err.Error(), "has not ended") {
		t.Fatalf("report of a running session: %v", err)
	}

	db, _ = sqlite.Open(ctx, testDB(t, dir))
	if err := session.NewService(db).End(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out, err := run(t, "report", "--data-dir", dir)
	if err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}
	if os.Getenv("AX_REPORT_OUT") != "" {
		_ = os.WriteFile(os.Getenv("AX_REPORT_OUT"), []byte(out), 0o600)
	}
	for _, want := range []string{"== SUMMARY ==", "8 students", "8 submitted", "Mean", "% RIGHT PER QUESTION", "q007",
		"== INCIDENTS ==", "There were no integrity signals", "SIMILAR ANSWERS", "LEFT AND GOT IT RIGHT", "SESSIONS OF THE EXAM", "this session"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report text missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatal("plain text output must not carry terminal colours")
	}

	out, err = run(t, "report", "--data-dir", dir, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rep report.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Summary.Submitted != 8 || len(rep.Questions) != 7 {
		t.Fatalf("json: %v %+v", err, rep.Summary)
	}
	if q := rep.Questions[0]; q.ID != "q001" || q.Rate != 1 {
		t.Fatalf("q001: %+v", q)
	}
}
