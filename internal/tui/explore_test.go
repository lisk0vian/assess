package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/i18n"
)

func pickItems() []PickItem {
	return []PickItem{
		{Key: "A-12", Label: "Ana García", Detail: "3B"},
		{Key: "A-13", Label: "Ana Gómez", Detail: "3A"},
		{Key: "B-07", Label: "Luis Pérez", Detail: "3B"},
	}
}

func TestPicker(t *testing.T) {
	p := NewPicker("¿Quisiste decir…?", pickItems())
	drive(p, keys("l", "u")...)
	if len(p.shown) != 1 || p.items[p.shown[0]].Key != "B-07" {
		t.Fatalf("typing filters: %v", p.shown)
	}
	drive(p, keys("backspace", "backspace", "down", "enter")...)
	if p.Chosen != 1 {
		t.Fatalf("chose %d, want the second", p.Chosen)
	}
	view := ansi.Strip(p.View().Content)
	if !strings.Contains(view, "A-12") || !strings.Contains(view, "Filtro:") {
		t.Fatalf("view:\n%s", view)
	}
	q := NewPicker("x", pickItems())
	drive(q, keys("esc")...)
	if q.Chosen != -1 {
		t.Fatal("Esc cancels")
	}
}

func exploreFixture() *ExploreData {
	d := &ExploreData{Vault: "aula · cifrada, abierta"}
	d.Tabs[TabStudents] = []ExploreRow{
		{ID: "s1", Key: "A-12", Label: "Ana García", Detail: "3B"},
		{ID: "s2", Key: "B-07", Label: "Luis Pérez", Detail: "3B"},
	}
	d.Tabs[TabClasses] = []ExploreRow{{ID: "c1", Key: "3B", Label: "2 alumnos"}}
	d.Details = func(_ int, id string) []string { return []string{"detalle de " + id} }
	return d
}

func TestExplore(t *testing.T) {
	m := NewExplore(exploreFixture(), ExploreState{Tab: TabStudents})
	drive(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	drive(m, keys("p", "e", "r")...)
	if len(m.shown) != 1 {
		t.Fatalf("filter as you type: %v", m.shown)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Luis Pérez") || strings.Contains(view, "Ana García") || !strings.Contains(view, "detalle de s2") {
		t.Fatalf("view:\n%s", view)
	}
	drive(m, keys("enter")...)
	if m.Action != ActOpen || m.State.ID != "s2" || m.State.Filter != "per" || m.State.Tab != TabStudents {
		t.Fatalf("enter = %q %+v", m.Action, m.State)
	}

	// Coming back keeps the place; Tab changes tabs; Ctrl+N asks for a new one.
	m = NewExplore(exploreFixture(), m.State)
	if r := m.selected(); r == nil || r.ID != "s2" {
		t.Fatalf("state restored: %+v", r)
	}
	drive(m, keys("esc")...) // clears the filter first
	if len(m.query) != 0 || m.Action != "" {
		t.Fatal("Esc clears the filter before leaving")
	}
	drive(m, keys("shift+tab")...)
	if m.tab != TabClasses {
		t.Fatalf("tab = %d", m.tab)
	}
	drive(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.Action != ActNew || m.State.Tab != TabClasses {
		t.Fatalf("ctrl+n = %q %+v", m.Action, m.State)
	}
}

func TestExploreInEnglish(t *testing.T) {
	if err := i18n.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = i18n.SetLanguage("es") }()
	m := NewExplore(&ExploreData{Vault: "default"}, ExploreState{Tab: TabSessions})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "No sessions yet") || !strings.Contains(view, "Students 0") {
		t.Fatalf("view:\n%s", view)
	}
}
