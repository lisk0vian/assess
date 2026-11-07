package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
)

// The list a teacher saves to fill in is one the import reads, in every language.
func TestClassListTemplateImports(t *testing.T) {
	defer func() { _ = i18n.SetLanguage(i18n.Fallback) }()
	for _, lang := range []string{"en", "es"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), T("people.template.file"))
		if err := writeTemplate(file); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(file)
		rows, enc, _, err := parseRoster(data)
		if err != nil || enc != "UTF-8" || len(rows) != 2 || rows[0].Ref != "A-01" || rows[0].Name != "Ana García" {
			t.Fatalf("%s: %+v %s %v", lang, rows, enc, err)
		}
	}
}

// A student in no class is offered a class first; one in every class cannot move.
func TestStudentActionsFollowClasses(t *testing.T) {
	p := &session.People{Classes: []*session.Class{{ID: "c1", Name: "3B"}, {ID: "c2", Name: "3C"}},
		Of: map[string][]string{"in1": {"c1"}, "in2": {"c1", "c2"}}}
	keys := func(id string) (out []string) {
		for _, it := range exploreActions(tui.TabStudents, id, nil, p) {
			out = append(out, it.Key)
		}
		return out
	}
	if k := keys("orphan"); k[0] != "enroll" || slices.Contains(k, "move") || slices.Contains(k, "unenroll") {
		t.Fatalf("orphan: %v", k)
	}
	if k := keys("in1"); !slices.Contains(k, "move") || !slices.Contains(k, "unenroll") {
		t.Fatalf("in one class: %v", k)
	}
	if k := keys("in2"); slices.Contains(k, "move") {
		t.Fatalf("in every class: %v", k)
	}
}
