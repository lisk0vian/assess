package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/tui"
)

// A vault with an exam, a class and a sitting reaches every section of the hub.
func TestHubReadsTheVault(t *testing.T) {
	dir := t.TempDir()
	db := testDB(t, dir)
	d := []string{"--data-dir", dir}
	ok(t, "", append([]string{"vault", "add", example}, d...)...)
	ok(t, "", append([]string{"class", "create", "3B"}, d...)...)
	ok(t, "", append([]string{"student", "add", "A-12", "Ana", "--class", "3B"}, d...)...)
	ok(t, "", append([]string{"student", "add", "B-07", "Luis", "--class", "3B"}, d...)...)
	sitExam(t, db, "3B", []string{"A-12 Ana", "B-07 Luis"})

	cmd := newRootCmd()
	cmd.SetContext(context.Background())
	data, err := exploreData(cmd, dir)
	if err != nil {
		t.Fatal(err)
	}
	homeRows(data, homeState{health: daemon.Crashed})
	for tab, want := range map[int]int{tui.TabHome: 5, tui.TabExams: 1, tui.TabClasses: 1, tui.TabStudents: 2, tui.TabSessions: 1, tui.TabVault: 4} {
		if got := len(data.Tabs[tab]); got != want {
			t.Fatalf("section %d: %d rows, want %d", tab, got, want)
		}
	}
	if data.Alert == "" {
		t.Fatal("a crashed server must be stated in full on Home")
	}
	class := data.Tabs[tui.TabClasses][0]
	if det := data.Details(tui.TabClasses, class.ID); det == nil || len(det.Tiles) != 2 {
		t.Fatalf("class detail: %+v", det)
	}
	book, err := data.Book(class.ID)
	if err != nil || len(book.Rows) != 2 || len(book.Exams) != 1 {
		t.Fatalf("book: %+v %v", book, err)
	}
	stu := data.Tabs[tui.TabStudents][0]
	det := data.Details(tui.TabStudents, stu.ID)
	if det == nil || !strings.Contains(det.Sub, stu.Key) || len(det.Facts) != 3 {
		t.Fatalf("student detail: %+v", det)
	}
	exam := data.Tabs[tui.TabExams][0]
	if det := data.Details(tui.TabExams, exam.ID); det == nil || len(det.Sections[0].Lines) != 1 {
		t.Fatalf("exam detail: %+v", det)
	}
	if det := data.Details(tui.TabVault, "vault"); det == nil || det.Sub == "" {
		t.Fatalf("vault detail: %+v", det)
	}
	if acts := data.Actions(tui.TabExams, exam.ID); len(acts) != 6 || acts[1].Key != "try" {
		t.Fatalf("exam actions: %+v", acts)
	}
}
