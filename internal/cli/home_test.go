package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
)

func values(st homeState) []string {
	var out []string
	for _, o := range homeOptions(st) {
		out = append(out, o.Value)
	}
	return out
}

func TestHomeOptions(t *testing.T) {
	fresh := homeState{health: daemon.NotRunning}
	if got := strings.Join(values(fresh), ","); got != "edit,new,start,explore,settings,quit" {
		t.Fatalf("fresh: %s", got)
	}
	open := &session.Session{ID: "s1", Code: "K7Q2XM"}
	ended := &session.Session{ID: "s0", Code: "OLD111", State: session.StateEnded}
	st := homeState{health: daemon.Crashed, open: open, lastEnded: ended, lastTitles: map[string]string{"s1": "Álgebra", "s0": "Historia"}}
	opts := homeOptions(st)
	if got := strings.Join(values(st), ","); got != "resume,edit,new,report,explore,settings,quit" {
		t.Fatalf("with an open session: %s (starting another must not be offered)", got)
	}
	if !strings.Contains(opts[0].Key, "Álgebra") || !strings.Contains(opts[0].Key, "K7Q2XM") || !strings.Contains(opts[3].Key, "Historia") {
		t.Fatalf("labels: %+v", opts)
	}
}

func TestFindExams(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a/uno", "b/c/dos", "invalid/roto"} {
		if err := exam.Scaffold(filepath.Join(root, p), exam.ScaffoldOptions{Title: filepath.Base(p)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := exam.Scaffold(filepath.Join(root, "x", "y", "z", "lejos"), exam.ScaffoldOptions{}); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, e := range findExams(root, 3) {
		titles = append(titles, e.title)
	}
	slices.Sort(titles)
	if strings.Join(titles, ",") != "dos,uno" {
		t.Fatalf("found %v (want uno and dos; not invalid/ nor deeper than 3 levels)", titles)
	}
	if found := findExams(filepath.Join("..", "..", "examples"), 3); len(found) != 9 {
		t.Fatalf("examples: %+v", found)
	}
}
