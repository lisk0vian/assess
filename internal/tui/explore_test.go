package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/config"
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
	mean := 62.5
	d := &ExploreData{Vault: "aula · cifrada, abierta", Status: "Ningún examen en marcha"}
	d.Tabs[TabHome] = []ExploreRow{{ID: "new", Label: "Crear un examen", Direct: "new"}}
	d.Tabs[TabExams] = []ExploreRow{{ID: "e1", Key: "e1", Label: "Listas y bucles", Cells: []string{"2", "2026-10-07"}, Mean: &mean}}
	d.Tabs[TabStudents] = []ExploreRow{
		{ID: "s1", Key: "A-12", Label: "Ana García", Detail: "3B"},
		{ID: "s2", Key: "B-07", Label: "Luis Pérez", Detail: "3B"},
	}
	d.Tabs[TabClasses] = []ExploreRow{{ID: "c1", Key: "3B", Label: "2 alumnos"}}
	d.Tabs[TabSessions] = []ExploreRow{{ID: "x1", Key: "2026-10-07", Label: "Listas y bucles", Cells: []string{"3B", "K7M2QP", "2/2"}, Mean: &mean, Direct: "report"}}
	d.Tabs[TabVault] = []ExploreRow{{ID: "backup", Label: "Hacer una copia", Direct: "backup"}}
	d.Details = func(tab int, id string) *Detail {
		switch tab {
		case TabClasses:
			return &Detail{Title: "3B", Sub: "2 alumnos", Tiles: []DetailTile{{Name: "Ana García", Percent: &mean}, {Name: "Luis Pérez"}}}
		case TabVault:
			return &Detail{Sub: "D:/aula", Facts: [][2]string{{"aula", "bóveda"}}, Sections: []DetailSection{{Head: "COPIAS", Lines: []DetailLine{{Text: "2026-10-07"}}}}}
		}
		return &Detail{Title: "detalle de " + id, Sections: []DetailSection{{Head: "SESIONES", Lines: []DetailLine{{Key: "10-07", Text: "Listas", Percent: &mean}}}}}
	}
	d.Actions = func(int, string) []MenuItem { return []MenuItem{{"edit", "Editar"}, {"book", "Libro de notas"}} }
	d.Book = func(string) (*Book, error) {
		return &Book{Title: "3B", Exams: []string{"Listas"}, Rows: []BookRow{{Name: "Ana García", Cells: []*float64{&mean}, Mean: &mean}}}, nil
	}
	return d
}

func TestExplore(t *testing.T) {
	m := NewExplore(exploreFixture(), ExploreState{Tab: TabStudents})
	drive(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	drive(m, keys("p", "e", "r")...)
	if len(m.shown) != 1 {
		t.Fatalf("filter as you type: %v", m.shown)
	}
	v := view(m)
	if !strings.Contains(v, "Luis Pérez") || strings.Contains(v, "Ana García") || !strings.Contains(v, "detalle de s2") {
		t.Fatalf("view:\n%s", v)
	}

	// Enter opens the menu on top; choosing returns the action.
	drive(m, keys("enter")...)
	if m.menu == nil || !strings.Contains(view(m), "Editar") {
		t.Fatalf("menu:\n%s", view(m))
	}
	drive(m, keys("enter")...)
	if m.Action != ActOpen || m.State.Act != "edit" || m.State.ID != "s2" || m.State.Filter != "per" || m.State.Tab != TabStudents {
		t.Fatalf("enter = %q %+v", m.Action, m.State)
	}

	// Coming back keeps the place.
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

func TestHubDirectActionsAndLetters(t *testing.T) {
	m := NewExplore(exploreFixture(), ExploreState{Tab: TabHome})
	drive(m, keys("enter")...)
	if m.State.Act != "new" {
		t.Fatalf("home runs its action: %+v", m.State)
	}
	s := NewExplore(exploreFixture(), ExploreState{Tab: TabSessions})
	drive(s, keys("enter")...)
	if s.State.Act != "report" {
		t.Fatalf("a finished session opens its report: %+v", s.State)
	}
	c := NewExplore(exploreFixture(), ExploreState{Tab: TabClasses})
	drive(c, keys("l")...)
	if c.book == nil || !strings.Contains(view(c), "Libro de notas") || !strings.Contains(view(c), "63 %") {
		t.Fatalf("L opens the gradebook:\n%s", view(c))
	}
	drive(c, keys("x")...)
	if c.State.Act != "gradebook" {
		t.Fatalf("X exports: %+v", c.State)
	}
	i := NewExplore(exploreFixture(), ExploreState{Tab: TabExams})
	drive(i, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if i.State.Act != "import" {
		t.Fatalf("Ctrl+O imports: %+v", i.State)
	}
	// Lists keep every letter for the filter.
	e := NewExplore(exploreFixture(), ExploreState{Tab: TabExams})
	drive(e, keys("q")...)
	if e.Action != "" || string(e.query) != "q" {
		t.Fatal("q filters in a list")
	}
}

func TestHubAlertIsReadWhole(t *testing.T) {
	d := exploreFixture()
	d.Status, d.Alert = "Sesión sin servidor", strings.Repeat("Puedes reanudarla y nadie pierde sus respuestas. ", 3)
	m := NewExplore(d, ExploreState{})
	drive(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	v := view(m)
	if strings.Contains(v, "…") || !strings.Contains(v, "Crear un examen") {
		t.Fatalf("the alert must not hide the actions:\n%s", v)
	}
}

func TestHubSearchFindsAcrossSections(t *testing.T) {
	m := NewExplore(exploreFixture(), ExploreState{Tab: TabHome})
	drive(m, keys("/", "luis")...)
	if len(m.hits) != 1 || !strings.Contains(view(m), "Luis Pérez") {
		t.Fatalf("hits %v\n%s", m.hits, view(m))
	}
	drive(m, keys("enter")...)
	if r := m.selected(); m.tab != TabStudents || r == nil || r.ID != "s2" {
		t.Fatalf("Enter goes to the row: tab %d %+v", m.tab, r)
	}
}

// Every row fills the window, in both languages, at any size and section.
func TestHubFitsTheWindow(t *testing.T) {
	defer func() { _ = i18n.SetLanguage("es") }()
	for _, lang := range []string{"es", "en"} {
		if err := i18n.SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, size := range [][2]int{{120, 36}, {80, 24}, {72, 16}, {160, 50}, {100, 30}} {
			for _, data := range []*ExploreData{exploreFixture(), {Vault: "x"}} {
				m := NewExplore(data, ExploreState{})
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				check := func(what string) {
					t.Helper()
					lines := strings.Split(m.View().Content, "\n")
					if len(lines) != size[1] {
						t.Fatalf("%s %v %s: %d rows", lang, size, what, len(lines))
					}
					for i, l := range lines {
						if w := ansi.StringWidth(l); w != size[0] {
							t.Fatalf("%s %v %s: row %d is %d cells", lang, size, what, i, w)
						}
					}
				}
				for tab := range TabCount {
					m.switchTab(tab)
					check("section")
					drive(m, keys("?")...)
					check("help")
					drive(m, keys("esc", "/", "a")...)
					check("search")
					drive(m, keys("esc", "enter")...)
					check("menu")
					m.menu, m.Action = nil, ""
				}
				m.switchTab(TabClasses)
				m.openBook("c1")
				check("book")
			}
		}
	}
}

func TestSettingsEditsAndCollectsChanges(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewSettings(cfg)
	drive(m, tea.WindowSizeMsg{Width: 120, Height: 36})
	for _, size := range [][2]int{{80, 24}, {72, 16}, {160, 50}, {120, 36}} {
		drive(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for range m.groups {
			for range m.keys() {
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) != size[1] || ansi.StringWidth(lines[3]) != size[0] {
					t.Fatalf("%v: %d rows, %d cells", size, len(lines), ansi.StringWidth(lines[3]))
				}
				drive(m, keys("enter")...)
				if len(strings.Split(m.View().Content, "\n")) != size[1] {
					t.Fatalf("%v: the editor changes the height", size)
				}
				drive(m, keys("esc", "down")...)
			}
			drive(m, keys("right")...)
		}
		m.group, m.sel = 0, 0
	}
	drive(m, keys("enter")...)
	if m.editing == nil {
		t.Fatal("Enter opens the editor")
	}
	if m.options != nil {
		drive(m, keys("right", "enter")...)
	} else {
		drive(m, keys("x", "enter")...)
	}
	if m.editing != nil || len(m.pending) != 1 {
		t.Fatalf("pending %v problem %q", m.pending, m.problem)
	}
	if !strings.Contains(view(m), "●") {
		t.Fatalf("a changed setting is marked:\n%s", view(m))
	}
	drive(m, keys("esc")...)
	if len(m.Changes) != 1 {
		t.Fatalf("Esc saves and goes back: %v", m.Changes)
	}
}
