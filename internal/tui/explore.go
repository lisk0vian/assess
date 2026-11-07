package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/search"
)

// Sections of the hub.
const (
	TabHome = iota
	TabExams
	TabClasses
	TabStudents
	TabSessions
	TabVault
	TabCount
)

var tabKeys = [TabCount]string{"hub.home", "hub.exams", "hub.classes", "hub.students", "hub.sessions", "hub.vault"}

// ExploreRow is one line of a section.
type ExploreRow struct {
	ID     string   // what the action receives
	Key    string   // identifier column
	Label  string   // main text
	Detail string   // muted context
	Search []string // extra text the filter matches
	Cells  []string // columns of the Exams and Sessions tables
	Mean   *float64 // percentage drawn at the end of the row
	Direct string   // Enter runs this action instead of opening the menu
}

// Detail is the panel of the selected row.
type Detail struct {
	Title, Sub string
	Facts      [][2]string // value and label
	Sections   []DetailSection
	Tiles      []DetailTile // a group's students, as on the live sheet
}

// DetailSection is a heading and its lines.
type DetailSection struct {
	Head  string
	Lines []DetailLine
}

// DetailLine is a row of a panel: a short key, its text, and a percentage.
type DetailLine struct {
	Key, Text string
	Percent   *float64
}

// DetailTile is one student of a group.
type DetailTile struct {
	Name    string
	Percent *float64
}

// MenuItem is one action of the selected row.
type MenuItem struct{ Key, Label string }

// Book is a class's students by exams.
type Book struct {
	Title string
	Exams []string
	Rows  []BookRow
}

// BookRow is one student; a nil cell means not submitted.
type BookRow struct {
	Name  string
	Cells []*float64
	Mean  *float64
}

// ExploreData is what the hub shows. The funcs read on demand, as a detail may
// need its own queries.
type ExploreData struct {
	Vault   string // name and state, for the header
	Status  string // what is happening now, top right
	Alert   string // what needs the teacher first, on Home
	Notice  string // why something could not be read
	Tabs    [TabCount][]ExploreRow
	Details func(tab int, id string) *Detail
	Actions func(tab int, id string) []MenuItem
	Book    func(id string) (*Book, error)
}

// Hub actions returned to the caller.
const (
	ActOpen = "open" // run State.Act on the selected row
	ActNew  = "new"  // Ctrl+N: add something to the current section
	ActQuit = ""
)

// ExploreState is where the teacher was, to come back to it.
type ExploreState struct {
	Tab    int
	Filter string
	ID     string // selected row
	Act    string // what to run on it
}

// Explore is the hub: six sections, a filter as you type, and everything
// behind Enter. It returns what to do next; the caller runs it and opens the
// hub again.
type Explore struct {
	data   *ExploreData
	tab    int
	query  []rune
	shown  []int
	cur    int
	width  int
	height int
	cache  map[string]*Detail

	menu     []MenuItem
	menuSel  int
	showHelp bool

	searching bool
	find      textField
	hits      []hit
	hitSel    int

	book    *Book
	bookID  string
	bookSel int
	notice  string

	Action string
	State  ExploreState
}

type hit struct {
	tab int
	row ExploreRow
}

// NewExplore opens on state.
func NewExplore(d *ExploreData, st ExploreState) *Explore {
	m := &Explore{data: d, tab: st.Tab, query: []rune(st.Filter), width: 120, height: 36, cache: map[string]*Detail{}, notice: d.Notice}
	m.filter()
	m.selectID(st.ID)
	return m
}

func (m *Explore) selectID(id string) {
	for i, idx := range m.shown {
		if m.data.Tabs[m.tab][idx].ID == id {
			m.cur = i
		}
	}
}

func (m *Explore) rows() []ExploreRow { return m.data.Tabs[m.tab] }

// filtered: sections without a filter may use letters as keys.
func (m *Explore) filtered() bool {
	return m.tab == TabExams || m.tab == TabStudents || m.tab == TabSessions
}

func (m *Explore) filter() {
	m.shown = m.shown[:0]
	q := string(m.query)
	for i, r := range m.rows() {
		fields := append([]string{r.Key, r.Label, r.Detail}, r.Search...)
		if q == "" || search.Score(q, fields...) > 0 {
			m.shown = append(m.shown, i)
		}
	}
	m.cur = 0
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
	m.State = ExploreState{Tab: m.tab, Filter: string(m.query), Act: m.State.Act}
	if r := m.selected(); r != nil {
		m.State.ID = r.ID
	}
	return m, tea.Quit
}

func (m *Explore) run(act string) (tea.Model, tea.Cmd) {
	m.State.Act = act
	return m.finish(ActOpen)
}

// Init asks for the terminal's background, which picks the theme.
func (m *Explore) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Update routes keys to whatever is open on top.
func (m *Explore) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case tea.PasteMsg:
		if m.searching {
			m.find.insert(strings.ReplaceAll(msg.Content, "\n", " "))
			m.runSearch()
		}
	case tea.KeyPressMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return m.finish(ActQuit)
		}
		switch {
		case m.showHelp:
			if k == "?" || k == "esc" {
				m.showHelp = false
			}
		case m.searching:
			return m.searchKey(msg)
		case m.book != nil:
			return m.bookKey(k)
		case m.menu != nil:
			return m.menuKey(k)
		default:
			return m.listKey(msg)
		}
	}
	return m, nil
}

func (m *Explore) listKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		if len(m.query) > 0 {
			m.query = m.query[:0]
			m.filter()
			return m, nil
		}
		return m.finish(ActQuit)
	case "enter":
		return m.open()
	case "ctrl+n":
		if m.tab == TabExams || m.tab == TabClasses || m.tab == TabStudents {
			return m.finish(ActNew)
		}
	case "ctrl+o": // not Ctrl+I: terminals send it as Tab
		if m.tab == TabExams {
			return m.run("import")
		}
	case "ctrl+r":
		if m.tab == TabSessions && m.selected() != nil {
			return m.run("results")
		}
	case "tab", "right":
		m.switchTab((m.tab + 1) % TabCount)
	case "shift+tab", "left":
		m.switchTab((m.tab + TabCount - 1) % TabCount)
	case "up":
		m.cur = max(0, m.cur-1)
	case "down":
		m.cur = min(max(0, len(m.shown)-1), m.cur+1)
	case "pgup":
		m.cur = max(0, m.cur-10)
	case "pgdown":
		m.cur = min(max(0, len(m.shown)-1), m.cur+10)
	case "home":
		m.cur = 0
	case "end":
		m.cur = max(0, len(m.shown)-1)
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.filter()
		}
	case "?":
		m.showHelp = true
	case "/":
		m.searching, m.find, m.hits, m.hitSel = true, textField{}, nil, 0
	default:
		switch {
		case len(k) == 2 && k[0] == 'f' && k[1] >= '1' && k[1] <= '6':
			m.switchTab(int(k[1] - '1'))
		case m.filtered() && msg.Text != "":
			m.query = append(m.query, []rune(msg.Text)...)
			m.filter()
		case k == "q" || k == "Q":
			return m.finish(ActQuit)
		case m.tab == TabClasses && (k == "l" || k == "L") && m.selected() != nil:
			m.openBook(m.selected().ID)
		case m.tab == TabClasses && (k == "i" || k == "I") && m.selected() != nil:
			return m.run("import")
		}
	}
	return m, nil
}

// open runs the row's action, or shows its menu.
func (m *Explore) open() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil {
		return m, nil
	}
	if r.Direct != "" {
		return m.run(r.Direct)
	}
	if m.data.Actions != nil {
		m.menu, m.menuSel = m.data.Actions(m.tab, r.ID), 0
	}
	return m, nil
}

func (m *Explore) menuKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc":
		m.menu = nil
	case "up":
		m.menuSel = max(0, m.menuSel-1)
	case "down":
		m.menuSel = min(len(m.menu)-1, m.menuSel+1)
	case "enter":
		act := m.menu[m.menuSel].Key
		m.menu = nil
		if act == "book" {
			m.openBook(m.selected().ID)
			return m, nil
		}
		return m.run(act)
	}
	return m, nil
}

func (m *Explore) openBook(id string) {
	if m.data.Book == nil {
		return
	}
	b, err := m.data.Book(id)
	if err != nil {
		m.notice = err.Error()
		return
	}
	m.book, m.bookID, m.bookSel = b, id, 0
}

func (m *Explore) bookKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q", "Q":
		m.book = nil
	case "up":
		m.bookSel = max(0, m.bookSel-1)
	case "down":
		m.bookSel = min(max(0, len(m.book.Rows)-1), m.bookSel+1)
	case "x", "X":
		m.book = nil
		return m.run("gradebook")
	case "c", "C":
		m.book = nil
		return m.run("gradebook_csv")
	}
	return m, nil
}

func (m *Explore) runSearch() {
	q := m.find.value()
	m.hits, m.hitSel = nil, 0
	if strings.TrimSpace(q) == "" {
		return
	}
	for _, tab := range []int{TabExams, TabClasses, TabStudents, TabSessions} {
		for _, r := range m.data.Tabs[tab] {
			if search.Score(q, append([]string{r.Key, r.Label, r.Detail}, r.Search...)...) > 0 {
				m.hits = append(m.hits, hit{tab, r})
			}
		}
	}
}

func (m *Explore) searchKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.searching = false
	case "up":
		m.hitSel = max(0, m.hitSel-1)
	case "down":
		m.hitSel = min(max(0, len(m.hits)-1), m.hitSel+1)
	case "enter":
		if len(m.hits) > 0 {
			h := m.hits[m.hitSel]
			m.searching = false
			m.query = m.query[:0]
			m.switchTab(h.tab)
			m.selectID(h.row.ID)
		}
	default:
		if m.find.key(k, false) {
			m.runSearch()
		}
	}
	return m, nil
}

func (m *Explore) switchTab(t int) {
	m.tab = t
	m.query = m.query[:0]
	m.filter()
	m.notice = ""
}

func (m *Explore) details(r *ExploreRow) *Detail {
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

func (m *Explore) keyTable() []keyGroup {
	k := func(key, label string, bar bool) keyItem { return keyItem{keyHint{key, label}, bar} }
	section := k("Tab", T("hub.key.section"), true)
	move := k("↑↓", T("hub.key.choose"), true)
	find := k("/", T("hub.key.search"), true)
	help := k("?", T("monitor.ui.key_help"), true)
	quit := k("Esc", T("editor.ui.key_exit"), false)
	if m.tab == TabHome || m.tab == TabVault {
		quit = k("Q", T("editor.ui.key_exit"), true)
	}
	switch {
	case m.searching:
		return []keyGroup{{T("hub.key.search"), []keyItem{k("↑↓", T("hub.key.choose"), true), k("Enter", T("hub.key.open"), true), k("Esc", T("hub.key.back"), true)}}}
	case m.book != nil:
		return []keyGroup{{T("hub.book"), []keyItem{k("Esc", T("hub.key.back"), true), k("↑↓", T("hub.key.choose"), true), k("X", "Excel", true), k("C", "CSV", true)}}}
	case m.menu != nil:
		return []keyGroup{{T("hub.key.actions"), []keyItem{k("↑↓", T("hub.key.choose"), true), k("Enter", T("hub.key.open"), true), k("Esc", T("hub.key.back"), true)}}}
	}
	items := []keyItem{section, move, k("Enter", T("hub.key.open"), true)}
	switch m.tab {
	case TabExams:
		items = append(items, k("Ctrl+N", T("hub.key.new_exam"), true), k("Ctrl+O", T("hub.key.import"), true))
	case TabClasses:
		items = append(items, k("Ctrl+N", T("hub.key.new_class"), true), k("L", T("hub.book_key"), true), k("I", T("hub.key.import_list"), true))
	case TabStudents:
		items = append(items, k("Ctrl+N", T("hub.key.new_student"), true))
	case TabSessions:
		items = append(items, k("Ctrl+R", T("hub.key.results"), true))
	}
	return []keyGroup{{T(tabKeys[m.tab]), append(items, find, help, quit)}}
}

func (m *Explore) barKeys() []keyHint {
	var out []keyHint
	for _, g := range m.keyTable() {
		for _, it := range g.items {
			if it.bar {
				out = append(out, it.keyHint)
			}
		}
	}
	return out
}

// RunExplore shows the hub and returns the teacher's choice.
func RunExplore(d *ExploreData, st ExploreState) (string, ExploreState, error) {
	m := NewExplore(d, st)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return ActQuit, st, err
	}
	return m.Action, m.State, nil
}

// View draws the hub on a grid the size of the window.
func (m *Explore) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	g := newGrid(w, h, rBg)
	if w < 72 || h < 16 {
		for i, l := range wrap(T("monitor.ui.too_small"), max(10, w-2)) {
			g.text(1, 1+i, l, style{fg: rInk, bg: rBg})
		}
	} else {
		m.screen(g)
	}
	v := tea.NewView(g.String())
	v.AltScreen = true
	v.WindowTitle = "Assess"
	return v
}

func (m *Explore) screen(g *grid) {
	b := rSurface
	g.fill(0, 0, g.w, 2, b)
	g.text(1, 0, "Assess", style{fg: rInk, bg: b, bold: true})
	g.text(1, 1, fit(m.data.Vault, g.w/2), style{fg: rInk2, bg: b})
	g.right(g.w-1, 0, fit(m.data.Status, g.w/2), style{fg: rInk2, bg: b})
	g.hline(0, 2, g.w, rRuleStrong, rBg)
	names := make([]string, TabCount)
	for i, key := range tabKeys {
		names[i] = strings.ToUpper(T(key))
	}
	tabBar(g, 3, names, m.tab)
	bottom := g.h - keyBar(g, m.barKeys())
	y := 4
	if m.notice != "" {
		bottom--
		g.fill(0, bottom, g.w, 1, rInverse)
		g.text(1, bottom, fit(m.notice, g.w-2), style{fg: rOnInverse, bg: rInverse, bold: true})
	}
	g.fill(0, y, g.w, bottom-y, b)
	if m.filtered() && m.book == nil {
		g.fill(0, y, g.w, 1, rSurface2)
		end := label(g, 1, y, T("hub.filter"), rSurface2) + 1
		end = g.text(end, y, fit(string(m.query), g.w/2), style{fg: rInk, bg: rSurface2, bold: true})
		g.put(end, y, "▌", style{fg: rAccent, bg: rSurface2})
		g.right(g.w-1, y, T("hub.n_shown", "n", len(m.shown), "total", len(m.rows())), style{fg: rInk2, bg: rSurface2})
		y++
	}
	H := bottom - y
	switch {
	case m.book != nil:
		m.drawBook(g, y, H)
	case m.tab == TabHome:
		m.drawHome(g, y, H)
	case m.tab == TabExams:
		m.drawExams(g, y, H)
	case m.tab == TabClasses:
		m.drawClasses(g, y, H)
	case m.tab == TabStudents:
		m.drawStudents(g, y, H)
	case m.tab == TabSessions:
		m.drawSessions(g, y, H)
	default:
		m.drawVault(g, y, H)
	}
	switch {
	case m.searching:
		m.drawSearch(g)
	case m.menu != nil:
		m.drawMenu(g)
	}
	if m.showHelp {
		helpPanel(g, m.keyTable())
	}
}

// selRow paints a list row; the selected one carries the magenta mark.
func selRow(g *grid, x, y, w int, on bool, bg role) role {
	if on {
		bg = rSurface2
	}
	g.fill(x, y, w, 1, bg)
	if on {
		g.put(x, y, "▒", style{fg: rAccent, bg: bg})
	}
	return bg
}

func nameStyle(on bool, bg role) style {
	if on {
		return style{fg: rAccentInk, bg: bg, bold: true}
	}
	return style{fg: rInk, bg: bg}
}

// window is the first row to show so that cur stays in view.
func window(cur, n, room int) int { return min(max(0, cur-room/2), max(0, n-room)) }

func percent(v float64) string { return strconv.Itoa(int(v+0.5)) + " %" }

// meanCell draws a percentage ending just before column end.
func meanCell(g *grid, end, y int, v *float64, bg role) {
	if v == nil {
		g.right(end, y, "—", style{fg: rInk2, bg: bg})
		return
	}
	g.right(end, y, percent(*v), style{fg: rInk, bg: bg, bold: true})
}

func (m *Explore) empty(g *grid, x, y, w int) {
	key := "hub.empty." + []string{"", "exams", "classes", "students", "sessions", ""}[m.tab]
	if len(m.query) > 0 {
		key = "pick.none"
	}
	para(g, x, y, w, T(key), style{fg: rInk2, bg: rSurface})
}

func (m *Explore) drawHome(g *grid, y, H int) {
	big := g.w >= wideCols
	w := g.w - 4
	if big {
		w = 58
	}
	x, bg := 2, rSurface
	g.fill(0, y, g.w, H, rBg)
	sheet(g, x, y+1, w, H-3)
	first, room := y+2, H-5
	if m.data.Alert != "" {
		n := 1 + len(wrap(m.data.Alert, w-4))
		plate(g, x, y+1, w, m.data.Status, m.data.Alert)
		first, room = first+n+1, room-n-1
	}
	top := window(m.cur, len(m.shown), room)
	for i := range min(room, len(m.shown)-top) {
		r := m.rows()[m.shown[top+i]]
		on, Y := top+i == m.cur, first+i
		rowBg := selRow(g, x, Y, w, on, bg)
		g.text(x+2, Y, fit(r.Label, w-4), nameStyle(on, rowBg))
	}
	if d := m.details(m.selected()); big && d != nil {
		X := x + w + 4
		m.drawDetail(g, X, y+1, g.w-X-2, H-2, d, rBg)
	}
}

func (m *Explore) drawDetail(g *grid, x, y, w, h int, d *Detail, bg role) {
	end := y + h
	Y := y
	if d.Title != "" {
		Y = para(g, x, Y, w, d.Title, style{fg: rInk, bg: bg, bold: true})
	}
	if d.Sub != "" {
		Y = para(g, x, Y, w, d.Sub, style{fg: rInk2, bg: bg})
	}
	if len(d.Facts) > 0 {
		record(g, x-2, Y, w+2, d.Facts, bg)
		Y += 4
	}
	for _, s := range d.Sections {
		Y++
		if Y >= end {
			return
		}
		label(g, x, Y, fit(s.Head, w), bg)
		Y += 2
		for _, l := range s.Lines {
			if Y >= end {
				return
			}
			tx := x
			if l.Key != "" {
				tx = x + 10
				g.text(x, Y, pad(l.Key, 9), style{fg: rInk2, bg: bg})
			}
			if l.Percent == nil {
				Y = para(g, tx, Y, x+w-tx, l.Text, style{fg: rInk, bg: bg})
				continue
			}
			tw := min((w-16)/2, 24)
			g.text(tx, Y, fit(l.Text, tw-1), style{fg: rInk, bg: bg})
			halfBar(g, tx+tw, Y, x+w-tx-tw-6, *l.Percent/100, rInk, bg)
			meanCell(g, x+w, Y, l.Percent, bg)
			Y++
		}
	}
}

func (m *Explore) drawExams(g *grid, y, H int) {
	big := g.w >= wideCols
	W := g.w
	if big {
		W = 76
	}
	bg := rSurface
	if len(m.rows()) == 0 || len(m.shown) == 0 {
		m.empty(g, 2, y+1, g.w-4)
		return
	}
	cols := []int{10, 11}
	nw := W - 2 - cols[0] - cols[1] - 7
	label(g, 2, y+1, pad(T("hub.col.exam"), nw)+pad(strings.ToUpper(T("hub.col.sessions")), cols[0])+pad(T("hub.col.last"), cols[1])+T("hub.col.mean"), bg)
	g.hline(0, y+2, W, rRuleStrong, bg)
	room := (H - 3) / 2
	top := window(m.cur, len(m.shown), room)
	for i := range min(room, len(m.shown)-top) {
		r := m.rows()[m.shown[top+i]]
		on, Y := top+i == m.cur, y+3+i*2
		rowBg := selRow(g, 0, Y, W, on, bg)
		g.text(2, Y, fit(r.Label, nw-1), nameStyle(on, rowBg))
		x := 2 + nw
		for j, c := range r.Cells {
			g.text(x, Y, fit(c, cols[j]-1), style{fg: rInk2, bg: rowBg})
			x += cols[j]
		}
		meanCell(g, W-2, Y, r.Mean, rowBg)
		g.hline(0, Y+1, W, rRule, bg)
	}
	if d := m.details(m.selected()); big && d != nil {
		g.vline(W+1, y+1, H-2, rRule, bg)
		m.drawDetail(g, W+3, y+1, g.w-W-5, H-2, d, bg)
	}
}

func (m *Explore) drawClasses(g *grid, y, H int) {
	big := g.w >= wideCols
	rw := 22
	if big {
		rw = 28
	}
	g.fill(0, y, rw, H, rBg)
	if len(m.rows()) == 0 {
		g.fill(0, y, g.w, H, rSurface)
		g.text(4, y+2, T("hub.no_classes"), style{fg: rInk, bg: rSurface, bold: true})
		para(g, 4, y+3, min(70, g.w-8), T("hub.no_classes_hint"), style{fg: rInk2, bg: rSurface})
		return
	}
	room := (H - 1) / 3
	top := window(m.cur, len(m.shown), room)
	for i := range min(room, len(m.shown)-top) {
		r := m.rows()[m.shown[top+i]]
		on, Y := top+i == m.cur, y+1+i*3
		rowBg := selRow(g, 0, Y, rw, on, rBg)
		selRow(g, 0, Y+1, rw, on, rBg)
		g.text(2, Y, fit(r.Key, rw-3), nameStyle(on, rowBg))
		g.text(2, Y+1, fit(r.Label, rw-3), style{fg: rInk2, bg: rowBg})
	}
	g.vline(rw, y, H, rRuleStrong, rSurface)
	d := m.details(m.selected())
	if d == nil {
		return
	}
	X := rw + 2
	end := g.text(X, y+1, fit(d.Title, g.w-X-2), style{fg: rInk, bg: rSurface, bold: true})
	g.text(end+2, y+1, fit(d.Sub, g.w-end-3), style{fg: rInk2, bg: rSurface})
	cols, tw := 2, (g.w-X-2)/2
	if big {
		cols, tw = 4, (g.w-X-1)/4-1
	}
	rows := (H - 3) / 4
	for i, t := range d.Tiles {
		TX, TY := X+(i%cols)*(tw+1), y+3+(i/cols)*4
		if i/cols >= rows {
			g.right(g.w-2, y+1, T("hub.more_students", "n", len(d.Tiles)-i), style{fg: rInk2, bg: rSurface})
			break
		}
		tb := rBg
		g.fill(TX, TY, tw, 3, tb)
		g.text(TX+1, TY, fit(t.Name, tw-2), style{fg: rInk, bg: tb, bold: true})
		if t.Percent == nil {
			g.text(TX+1, TY+1, fit(T("hub.no_grades"), tw-2), style{fg: rInk2, bg: tb})
			continue
		}
		halfBar(g, TX+1, TY+1, tw-8, *t.Percent/100, rInk, tb)
		meanCell(g, TX+tw-1, TY+1, t.Percent, tb)
	}
}

func (m *Explore) drawStudents(g *grid, y, H int) {
	rw := 26
	if g.w >= wideCols {
		rw = 34
	}
	g.fill(0, y, rw, H, rBg)
	if len(m.shown) == 0 {
		g.fill(0, y, g.w, H, rSurface)
		m.empty(g, 2, y+1, g.w-4)
		return
	}
	top := window(m.cur, len(m.shown), H)
	for i := range min(H, len(m.shown)-top) {
		r := m.rows()[m.shown[top+i]]
		on, Y := top+i == m.cur, y+i
		rowBg := selRow(g, 0, Y, rw, on, rBg)
		g.text(2, Y, pad(r.Key, 7), style{fg: rInk2, bg: rowBg})
		g.text(9, Y, fit(r.Label, rw-10), nameStyle(on, rowBg))
	}
	g.vline(rw, y, H, rRuleStrong, rSurface)
	if d := m.details(m.selected()); d != nil {
		m.drawDetail(g, rw+2, y+1, g.w-rw-3, H-2, d, rSurface)
	}
}

func (m *Explore) drawSessions(g *grid, y, H int) {
	big := g.w >= wideCols
	bg := rSurface
	if len(m.shown) == 0 {
		m.empty(g, 2, y+1, g.w-4)
		return
	}
	cols := []int{14, 10, 8}
	dw, bar := 13, 0
	if big {
		cols, bar = []int{14, 10, 10}, min(24, g.w-100)
	}
	nw := g.w - 2 - dw - cols[0] - cols[1] - cols[2] - 7 - bar
	label(g, 2, y+1, pad(T("hub.col.date"), dw)+pad(T("hub.col.exam"), nw)+pad(T("hub.col.group"), cols[0])+pad(T("hub.col.code"), cols[1])+pad(T("hub.col.submitted"), cols[2])+T("hub.col.mean"), bg)
	g.hline(0, y+2, g.w, rRuleStrong, bg)
	room := (H - 4) / 2
	top := window(m.cur, len(m.shown), room)
	for i := range min(room, len(m.shown)-top) {
		r := m.rows()[m.shown[top+i]]
		on, Y := top+i == m.cur, y+3+i*2
		rowBg := selRow(g, 0, Y, g.w, on, bg)
		g.text(2, Y, fit(r.Key, dw-1), style{fg: rInk2, bg: rowBg})
		g.text(2+dw, Y, fit(r.Label, nw-1), nameStyle(on, rowBg))
		x := 2 + dw + nw
		for j, c := range r.Cells {
			g.text(x, Y, fit(c, cols[j]-1), style{fg: rInk2, bg: rowBg})
			x += cols[j]
		}
		if r.Mean != nil && bar > 0 {
			halfBar(g, x+7, Y, bar-1, *r.Mean/100, rInk, rowBg)
		}
		meanCell(g, x+6, Y, r.Mean, rowBg)
		g.hline(0, Y+1, g.w, rRule, bg)
	}
	if d := m.details(m.selected()); d != nil && d.Sub != "" {
		g.text(2, y+H-1, fit(d.Sub, g.w-4), style{fg: rInk2, bg: bg})
	}
}

func (m *Explore) drawVault(g *grid, y, H int) {
	big := g.w >= wideCols
	bg := rSurface
	d := m.details(&ExploreRow{ID: "vault"})
	if d == nil {
		d = &Detail{}
	}
	Y := y + 1
	if len(d.Facts) > 0 {
		record(g, 0, Y, g.w, d.Facts, bg)
		Y += 3
	}
	if d.Sub != "" {
		g.text(1, Y, fit(d.Sub, g.w-2), style{fg: rInk2, bg: bg})
	}
	Y += 2
	w := g.w
	if big {
		w = 60
	}
	label(g, 2, Y, T("hub.actions"), bg)
	Y += 2
	for i, idx := range m.shown {
		r := m.rows()[idx]
		if Y+i >= y+H {
			break
		}
		on := i == m.cur
		rowBg := selRow(g, 0, Y+i, w, on, bg)
		g.text(2, Y+i, fit(r.Label, w-4), nameStyle(on, rowBg))
	}
	if big && len(d.Sections) > 0 {
		top := Y - 2
		g.vline(62, top, y+H-top-1, rRule, bg)
		m.drawDetail(g, 64, top-1, g.w-66, y+H-top, &Detail{Sections: d.Sections}, bg)
	}
}

func (m *Explore) drawMenu(g *grid) {
	r := m.selected()
	w := min(46, g.w-4)
	h := len(m.menu) + 4
	x, y := (g.w-w)/2, max(4, (g.h-h)/2-1)
	panel(g, x, y, w, h, fit(r.Label, w-4))
	for i, it := range m.menu {
		Y := y + 3 + i
		on := i == m.menuSel
		rowBg := selRow(g, x, Y, w, on, rSurface)
		g.text(x+2, Y, fit(it.Label, w-4), nameStyle(on, rowBg))
	}
}

func (m *Explore) drawSearch(g *grid) {
	w := min(84, g.w-4)
	h := min(21, g.h-6)
	x, y := (g.w-w)/2, 3
	sheet(g, x, y, w, h)
	g.hline(x, y, w, rRuleStrong, rSurface)
	m.find.draw(g, x+2, y+1, w-4, 1, true, style{fg: rInk, bg: rSurface2, bold: true})
	if len(m.hits) == 0 {
		hint := T("hub.search_hint")
		if m.find.value() != "" {
			hint = T("pick.none")
		}
		para(g, x+2, y+3, w-4, hint, style{fg: rInk2, bg: rSurface})
		return
	}
	Y, room := y+3, h-4
	top := window(m.hitSel, len(m.hits), room)
	for i := range min(room, len(m.hits)-top) {
		c := m.hits[top+i]
		on := top+i == m.hitSel
		rowBg := selRow(g, x, Y+i, w, on, rSurface)
		name := c.row.Label
		if c.tab == TabClasses {
			name = c.row.Key
		}
		g.text(x+2, Y+i, fit(name, w-24), nameStyle(on, rowBg))
		g.right(x+w-2, Y+i, fit(T(tabKeys[c.tab]), 20), style{fg: rInk2, bg: rowBg})
	}
}

func (m *Explore) drawBook(g *grid, y, H int) {
	big := g.w >= wideCols
	b, bg := m.book, rSurface
	nw, cw := 20, 7
	if big {
		nw, cw = 28, 11
	}
	end := g.text(1, y+1, T("hub.book")+" · "+b.Title, style{fg: rInk, bg: bg, bold: true})
	g.text(end+2, y+1, fit(T("hub.book_hint"), g.w-end-3), style{fg: rInk2, bg: bg})
	shown := min(len(b.Exams), max(1, (g.w-2-nw-7)/cw))
	x := 2 + nw
	label(g, 2, y+3, pad(T("hub.col.student"), nw), bg)
	for i, e := range b.Exams[:shown] {
		g.text(x+i*cw, y+3, fit(e, cw-1), style{fg: rInk2, bg: bg, bold: true})
	}
	g.text(x+shown*cw, y+3, T("hub.col.mean"), style{fg: rInk, bg: bg, bold: true})
	g.hline(0, y+4, g.w, rRuleStrong, bg)
	room := max(1, H-7)
	top := window(m.bookSel, len(b.Rows), room)
	for i := range min(room, len(b.Rows)-top) {
		r := b.Rows[top+i]
		on, Y := top+i == m.bookSel, y+5+i
		rowBg := selRow(g, 0, Y, g.w, on, bg)
		g.text(2, Y, fit(r.Name, nw-1), nameStyle(on, rowBg))
		for j, v := range r.Cells[:shown] {
			if v == nil {
				g.text(x+j*cw, Y, "—", style{fg: rInk2, bg: rowBg})
				continue
			}
			st := style{fg: rInk2, bg: rowBg}
			if *v < 50 { // a reading aid, not a pass mark
				st = style{fg: rInk, bg: rowBg, bold: true}
			}
			g.text(x+j*cw, Y, percent(*v), st)
		}
		meanCell(g, x+shown*cw+ansi.StringWidth(T("hub.col.mean"))+2, Y, r.Mean, rowBg)
	}
	g.text(1, y+H-1, fit(T("hub.book_note"), g.w-2), style{fg: rInk2, bg: bg})
}
