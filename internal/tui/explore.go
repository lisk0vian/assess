package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/search"
)

// Explore tabs.
const (
	TabExams = iota
	TabClasses
	TabStudents
	TabSessions
	TabCount
)

var tabKeys = [TabCount]string{"explore.tab.exams", "explore.tab.classes", "explore.tab.students", "explore.tab.sessions"}

// ExploreRow is one line of a tab.
type ExploreRow struct {
	ID     string   // what the action receives
	Key    string   // identifier column
	Label  string   // main text
	Detail string   // muted context
	Search []string // extra text the filter matches
}

// ExploreData is what Explore shows; Details renders the side panel of a
// row on demand (a student's history, a class's members…).
type ExploreData struct {
	Vault   string // name and state, for the header
	Tabs    [TabCount][]ExploreRow
	Details func(tab int, id string) []string
}

// Explore actions returned to the caller.
const (
	ActOpen = "open" // Enter: the actions of the selected row
	ActNew  = "new"  // Ctrl+N: add something to the current tab
	ActQuit = ""
)

// ExploreState is where the teacher was, to come back to it.
type ExploreState struct {
	Tab    int
	Filter string
	ID     string // selected row
}

// Explore browses the vault: four tabs, a filter as you type and the
// details of the selected row. It returns what to do next; the caller runs
// the forms and opens Explore again.
type Explore struct {
	data   *ExploreData
	tab    int
	query  []rune
	shown  []int
	cur    int
	top    int
	width  int
	height int
	cache  map[string][]string

	Action string
	State  ExploreState
}

// NewExplore opens on state.
func NewExplore(d *ExploreData, st ExploreState) *Explore {
	m := &Explore{data: d, tab: st.Tab, query: []rune(st.Filter), width: 100, height: 30, cache: map[string][]string{}}
	m.filter()
	for i, idx := range m.shown {
		if d.Tabs[m.tab][idx].ID == st.ID {
			m.cur = i
		}
	}
	return m
}

func (m *Explore) rows() []ExploreRow { return m.data.Tabs[m.tab] }

func (m *Explore) filter() {
	m.shown = m.shown[:0]
	q := string(m.query)
	for i, r := range m.rows() {
		fields := append([]string{r.Key, r.Label, r.Detail}, r.Search...)
		if q == "" || search.Score(q, fields...) > 0 {
			m.shown = append(m.shown, i)
		}
	}
	m.cur, m.top = 0, 0
}

func (m *Explore) selected() *ExploreRow {
	if len(m.shown) == 0 {
		return nil
	}
	r := m.rows()[m.shown[m.cur]]
	return &r
}

func (m *Explore) finish(action string) (tea.Model, tea.Cmd) {
	m.Action = action
	m.State = ExploreState{Tab: m.tab, Filter: string(m.query)}
	if r := m.selected(); r != nil {
		m.State.ID = r.ID
	}
	return m, tea.Quit
}

// Init does nothing.
func (m *Explore) Init() tea.Cmd { return nil }

// Update filters, moves and chooses.
func (m *Explore) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case "ctrl+c":
			return m.finish(ActQuit)
		case "esc":
			if len(m.query) > 0 {
				m.query = m.query[:0]
				m.filter()
				return m, nil
			}
			return m.finish(ActQuit)
		case "enter":
			if m.selected() != nil {
				return m.finish(ActOpen)
			}
		case "ctrl+n":
			if m.tab != TabSessions {
				return m.finish(ActNew)
			}
		case "tab", "right":
			m.switchTab((m.tab + 1) % TabCount)
		case "shift+tab", "left":
			m.switchTab((m.tab + TabCount - 1) % TabCount)
		case "up", "ctrl+p":
			if m.cur > 0 {
				m.cur--
			}
		case "down":
			if m.cur < len(m.shown)-1 {
				m.cur++
			}
		case "pgup":
			m.cur = max(0, m.cur-m.listRoom())
		case "pgdown":
			m.cur = min(max(0, len(m.shown)-1), m.cur+m.listRoom())
		case "home":
			m.cur = 0
		case "end":
			m.cur = max(0, len(m.shown)-1)
		case "backspace":
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.filter()
			}
		default:
			if len(k) == 2 && k[0] == 'f' && k[1] >= '1' && k[1] <= '4' {
				m.switchTab(int(k[1] - '1'))
			} else if msg.Text != "" {
				m.query = append(m.query, []rune(msg.Text)...)
				m.filter()
			}
		}
	}
	room := m.listRoom()
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+room {
		m.top = m.cur - room + 1
	}
	return m, nil
}

func (m *Explore) switchTab(t int) {
	m.tab = t
	m.filter()
}

func (m *Explore) listRoom() int { return max(3, m.height-6) }

func (m *Explore) details(r *ExploreRow) []string {
	if m.data.Details == nil || r == nil {
		return nil
	}
	key := strconv.Itoa(m.tab) + "/" + r.ID
	if d, ok := m.cache[key]; ok {
		return d
	}
	d := m.data.Details(m.tab, r.ID)
	m.cache[key] = d
	return d
}

// View draws the tabs, the filter, the list and the details.
func (m *Explore) View() tea.View {
	w := max(60, m.width)
	var b strings.Builder
	head := brandStyle.Render("ASSESS") + "  " + mutedStyle.Render(T("explore.title")+" · "+m.data.Vault)
	b.WriteString(head + "\n")
	var tabs []string
	for i, k := range tabKeys {
		label := T(k, "n", len(m.data.Tabs[i]))
		if i == m.tab {
			tabs = append(tabs, selStyle.Render(" "+label+" "))
		} else {
			tabs = append(tabs, mutedStyle.Render(" "+label+" "))
		}
	}
	b.WriteString(strings.Join(tabs, " ") + "\n")
	filter := mutedStyle.Render(T("pick.filter")) + " " + string(m.query) + accentStyle.Render("▏")
	if len(m.query) > 0 {
		filter += "  " + mutedStyle.Render(T("explore.matches", "n", len(m.shown)))
	}
	b.WriteString(filter + "\n")

	listW := w
	sel := m.selected()
	det := m.details(sel)
	if w >= 100 && len(det) > 0 {
		listW = w * 11 / 20
	}
	room := m.listRoom()
	keyW := 0
	for _, i := range m.shown {
		keyW = max(keyW, lipgloss.Width(m.rows()[i].Key))
	}
	keyW = min(keyW, 18)
	var list []string
	if len(m.shown) == 0 {
		empty := T("explore.empty." + []string{"exams", "classes", "students", "sessions"}[m.tab])
		if len(m.query) > 0 {
			empty = T("pick.none")
		}
		list = append(list, mutedStyle.Render(empty))
	}
	end := min(len(m.shown), m.top+room)
	for row := m.top; row < end; row++ {
		r := m.rows()[m.shown[row]]
		key := ansi.Truncate(r.Key, keyW, "…")
		key += strings.Repeat(" ", keyW-lipgloss.Width(key))
		text := key + "  " + r.Label
		if r.Detail != "" {
			text += "  " + mutedStyle.Render(r.Detail)
		}
		text = ansi.Truncate(text, listW-3, "…")
		if row == m.cur {
			list = append(list, accentStyle.Render("›")+" "+selStyle.Render(ansi.Strip(text)))
		} else {
			list = append(list, "  "+text)
		}
	}
	for len(list) < room {
		list = append(list, "")
	}
	if listW < w {
		var right []string
		for _, l := range det {
			right = append(right, ansi.Truncate(l, w-listW-3, "…"))
		}
		for len(right) < room {
			right = append(right, "")
		}
		right = right[:room]
		left := lipgloss.NewStyle().Width(listW).Render(strings.Join(list, "\n"))
		sep := mutedStyle.Render(strings.Repeat("│\n", room-1) + "│")
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, sep, " "+strings.Join(right, "\n ")) + "\n")
	} else {
		b.WriteString(strings.Join(list, "\n") + "\n")
	}
	k := func(key, label string) string { return keyStyle.Render(key) + " " + label }
	keys := []string{k("Tab", T("explore.key.tabs")), k("↑↓", T("pick.key.move")), k("Enter", T("explore.key.actions"))}
	if m.tab != TabSessions {
		keys = append(keys, k("Ctrl+N", T("explore.key.new."+[]string{"exams", "classes", "students"}[m.tab])))
	}
	keys = append(keys, k("Esc", T("explore.key.quit")))
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)) + "\n" + strings.Join(keys, "   "))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// RunExplore shows Explore and returns the teacher's choice.
func RunExplore(d *ExploreData, st ExploreState) (string, ExploreState, error) {
	m := NewExplore(d, st)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return ActQuit, st, err
	}
	return m.Action, m.State, nil
}
