package tui

import (
	"math"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/i18n"
)

// Screens around a session: the first use, preparing a session and
// recovering one. Each does one job and hands the result back to the caller.

// plainShell is a title strip and the key bar; it returns the body's rows.
func plainShell(g *grid, title, sub string, keys []keyHint) (y, h int) {
	b := rSurface
	g.fill(0, 0, g.w, 2, b)
	g.text(1, 0, fit(title, g.w-2), style{fg: rInk, bg: b, bold: true})
	g.text(1, 1, fit(sub, g.w-2), style{fg: rInk2, bg: b})
	g.hline(0, 2, g.w, rRuleStrong, rBg)
	bottom := g.h - keyBar(g, keys)
	g.fill(0, 3, g.w, bottom-3, rBg)
	return 3, bottom - 3
}

func screenView(w, h int, draw func(g *grid)) tea.View {
	w, h = max(1, w), max(1, h)
	g := newGrid(w, h, rBg)
	if w < 72 || h < 16 {
		for i, l := range wrap(T("monitor.ui.too_small"), max(10, w-2)) {
			g.text(1, 1+i, l, style{fg: rInk, bg: rBg})
		}
	} else {
		draw(g)
	}
	v := tea.NewView(g.String())
	v.AltScreen = true
	v.WindowTitle = "Assess"
	return v
}

// check is one line of a checklist: done, or what to do about it.
func check(g *grid, x, y, w int, ok bool, text string, bg role) int {
	if ok {
		g.text(x, y, "✓", style{fg: rInk, bg: bg, bold: true})
		return para(g, x+2, y, w-2, text, style{fg: rInk, bg: bg})
	}
	rows := wrap("! "+text, w-2)
	g.fill(x-2, y, w+2, len(rows), rInverse)
	for i, l := range rows {
		g.text(x, y+i, l, style{fg: rOnInverse, bg: rInverse, bold: true})
	}
	return y + len(rows)
}

// ---------------- first use ----------------

// FirstRun takes a teacher from the first `ax` to an empty vault: welcome,
// the vault and its password, and the recovery key, shown once.
type FirstRun struct {
	step          int
	width, height int
	lang          int
	langs         []string
	name, pw, pw2 textField
	field         int
	minPassword   int
	problem       string
	recovery      string

	// SetLanguage changes the interface language; Create makes the vault and
	// returns its recovery key ("" for a vault without password).
	SetLanguage func(lang string) error
	Create      func(name, password string) (string, error)

	// Done reports that the vault exists.
	Done bool
}

// NewFirstRun opens on the welcome.
func NewFirstRun(minPassword int) *FirstRun {
	m := &FirstRun{width: 120, height: 36, name: newField("default"), minPassword: minPassword}
	for _, c := range i18n.Languages() {
		m.langs = append(m.langs, c.Lang)
		if c.Lang == i18n.Language() {
			m.lang = len(m.langs) - 1
		}
	}
	return m
}

// Init asks for the terminal's background, which picks the theme.
func (m *FirstRun) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Update moves through the three screens.
func (m *FirstRun) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case tea.KeyPressMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.step {
		case 0:
			switch k {
			case "enter":
				m.step = 1
			case "left", "right":
				d := 1
				if k == "left" {
					d = -1
				}
				m.lang = (m.lang + d + len(m.langs)) % len(m.langs)
				if m.SetLanguage != nil {
					_ = m.SetLanguage(m.langs[m.lang])
				}
			case "esc", "q", "Q":
				return m, tea.Quit
			}
		case 1:
			return m, m.vaultKey(msg)
		case 2:
			if k == "enter" {
				m.Done = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *FirstRun) fields() []*textField { return []*textField{&m.name, &m.pw, &m.pw2} }

func (m *FirstRun) vaultKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.step, m.problem = 0, ""
	case "up", "shift+tab":
		m.field = max(0, m.field-1)
	case "down", "tab":
		m.field = min(2, m.field+1)
	case "ctrl+u":
		return m.create("")
	case "enter":
		if m.field < 2 {
			m.field++
			return nil
		}
		switch pw := m.pw.value(); {
		case len([]rune(pw)) < m.minPassword: // hidden text cannot be fixed: both start again
			m.problem, m.field, m.pw, m.pw2 = T("setup.pw_short", "n", m.minPassword), 1, textField{}, textField{}
		case pw != m.pw2.value():
			m.problem, m.field, m.pw, m.pw2 = T("setup.pw_differ"), 1, textField{}, textField{}
		default:
			return m.create(pw)
		}
	default:
		m.fields()[m.field].key(msg, false)
		m.problem = ""
	}
	return nil
}

func (m *FirstRun) create(pw string) tea.Cmd {
	name := strings.TrimSpace(m.name.value())
	if name == "" {
		m.problem, m.field = T("explore.required"), 0
		return nil
	}
	key, err := m.Create(name, pw)
	if err != nil {
		m.problem = err.Error()
		return nil
	}
	if key == "" {
		m.Done = true
		return tea.Quit
	}
	m.recovery, m.step = key, 2
	return nil
}

func (m *FirstRun) keys() []keyHint {
	switch m.step {
	case 0:
		return []keyHint{{"Enter", T("setup.key.begin")}, {"←→", T("setup.key.language")}, {"Esc", T("editor.ui.key_exit")}}
	case 1:
		return []keyHint{{"↑↓", T("editor.ui.key_field")}, {"Enter", T("setup.key.create")}, {"Ctrl+U", T("setup.key.no_password")}, {"Esc", T("hub.key.back")}}
	}
	return []keyHint{{"Enter", T("setup.key.saved_it")}}
}

// View draws the current screen.
func (m *FirstRun) View() tea.View {
	return screenView(m.width, m.height, func(g *grid) {
		subs := []string{T("setup.first_time"), T("setup.step", "n", 1, "of", 2), T("setup.step", "n", 2, "of", 2)}
		titles := []string{"Assess", T("setup.vault_title"), T("setup.recovery_title")}
		y, H := plainShell(g, titles[m.step], subs[m.step], m.keys())
		switch m.step {
		case 0:
			m.drawWelcome(g, y, H)
		case 1:
			m.drawVault(g, y, H)
		default:
			m.drawRecovery(g, y, H)
		}
	})
}

func (m *FirstRun) drawWelcome(g *grid, y, H int) {
	w := min(76, g.w-6)
	x, b := (g.w-w)/2, rSurface
	sheet(g, x, y+1, w, H-3)
	Y := para(g, x+3, y+3, w-6, T("setup.hello"), style{fg: rInk, bg: b, bold: true}) + 1
	Y = para(g, x+3, Y, w-6, T("setup.what"), style{fg: rInk, bg: b}) + 1
	for i, key := range []string{"setup.one", "setup.two", "setup.three"} {
		g.text(x+3, Y, " "+strconv.Itoa(i+1)+" ", style{fg: rOnInverse, bg: rInverse, bold: true})
		Y = para(g, x+7, Y, w-10, T(key), style{fg: rInk, bg: b})
	}
	names := make([]string, len(m.langs))
	for i, l := range m.langs {
		names[i] = languageName(l)
	}
	Y = chips(g, x+3, Y+1, names, m.lang, true, b) + 2
	para(g, x+3, Y, w-6, T("setup.halfway"), style{fg: rInk2, bg: b})
}

func (m *FirstRun) drawVault(g *grid, y, H int) {
	big := g.w >= wideCols
	w, x, b := g.w-4, 2, rSurface
	if big {
		w = 68
	}
	sheet(g, x, y+1, w, H-3)
	fx := x + 16
	labels := []string{T("setup.name"), T("setup.password"), T("setup.repeat")}
	for i, f := range m.fields() {
		Y := y + 3 + i*2
		label(g, x+2, Y, fit(labels[i], 13), b)
		value := *f
		if i > 0 {
			value = newField(strings.Repeat("*", len(f.r)))
		}
		value.draw(g, fx, Y, w-18, 1, i == m.field, style{fg: rInk, bg: rSurface2, bold: true})
	}
	Y := y + 10
	okLen := len(m.pw.r) >= m.minPassword
	mark := "·"
	if okLen {
		mark = "✓"
	}
	g.text(fx, Y-1, mark, style{fg: rInk, bg: b, bold: true})
	g.text(fx+2, Y-1, fit(T("setup.pw_rule", "n", m.minPassword), w-20), style{fg: rInk2, bg: b})
	if m.problem != "" {
		g.fill(x, Y, w, 1, rInverse)
		g.text(x+2, Y, fit(m.problem, w-4), style{fg: rOnInverse, bg: rInverse, bold: true})
	}
	g.hline(x, Y+2, w, rRule, b)
	para(g, x+2, Y+3, w-4, T("setup.pw_why"), style{fg: rInk2, bg: b})
	if !big {
		return
	}
	X, c := x+w+4, g.w-x-w-6
	label(g, X, y+2, fit(T("setup.keeps"), c), rBg)
	Y = y + 4
	for _, key := range []string{"setup.keeps_1", "setup.keeps_2", "setup.keeps_3", "setup.keeps_4"} {
		Y = para(g, X, Y, c, "· "+T(key), style{fg: rInk, bg: rBg})
	}
	label(g, X, Y+1, fit(T("setup.why_encrypt"), c), rBg)
	para(g, X, Y+3, c, T("setup.why_encrypt_text"), style{fg: rInk, bg: rBg})
}

func (m *FirstRun) drawRecovery(g *grid, y, H int) {
	w := min(76, g.w-6)
	x, b := (g.w-w)/2, rSurface
	sheet(g, x, y+1, w, H-3)
	Y := para(g, x+3, y+3, w-6, T("setup.keep_key"), style{fg: rInk, bg: b, bold: true})
	Y = para(g, x+3, Y, w-6, T("setup.key_opens"), style{fg: rInk2, bg: b}) + 1
	g.fill(x+3, Y, w-6, 3, rSurface2)
	para(g, x+5, Y+1, w-10, m.recovery, style{fg: rInk, bg: rSurface2, bold: true})
	Y += 4
	rows := wrap(T("setup.key_once"), w-10)
	g.fill(x+3, Y, w-6, len(rows)+2, rInverse)
	for i, l := range rows {
		g.text(x+5, Y+1+i, l, style{fg: rOnInverse, bg: rInverse, bold: true})
	}
	para(g, x+3, Y+len(rows)+3, w-6, T("setup.key_lost"), style{fg: rInk2, bg: b})
}

// ---------------- preparing a session ----------------

// FirewallState is what the firewall check says, in words for the screen.
type FirewallState struct {
	Supported, Ready bool
	Text             string
}

// PrepareData is what the session needs and what was checked.
type PrepareData struct {
	ExamTitle string
	Duration  time.Duration // the exam's own
	Problems  []string      // errors that stop the exam
	Classes   []string
	Members   []int // students per class
	Addrs     []string
	Firewall  FirewallState
	OpenPort  func() FirewallState // asks the system to open the port
}

// Prepare chooses who takes the exam and for how long, and checks what can
// fail before students wait. Opening the room only lets them in.
type Prepare struct {
	d             *PrepareData
	width, height int
	field         int
	class         int // 0 is no class
	rosterOnly    bool
	dur           int // 0 is the exam's own duration
	busy          bool
	ignore        bool // carry on with the firewall closed

	Open       bool
	Class      string
	RosterOnly bool
	Duration   time.Duration // 0 keeps the exam's
}

var prepareDurations = []time.Duration{0, 30 * time.Minute, 45 * time.Minute, time.Hour, 90 * time.Minute}

type firewallMsg FirewallState

// NewPrepare opens on the session's choices.
func NewPrepare(d *PrepareData) *Prepare { return &Prepare{d: d, width: 120, height: 36} }

// Init asks for the terminal's background, which picks the theme.
func (m *Prepare) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m *Prepare) blocked() bool { return len(m.d.Problems) > 0 }

// Update handles the choices and the checks' actions.
func (m *Prepare) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case firewallMsg:
		m.d.Firewall, m.busy = FirewallState(msg), false
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case "esc", "ctrl+c", "q", "Q":
			return m, tea.Quit
		case "up":
			m.field = max(0, m.field-1)
		case "down":
			m.field = min(2, m.field+1)
		case "left", "right":
			d := 1
			if k == "left" {
				d = -1
			}
			switch m.field {
			case 0:
				m.class = (m.class + d + len(m.d.Classes) + 1) % (len(m.d.Classes) + 1)
				if m.class == 0 {
					m.rosterOnly = false
				}
			case 1:
				m.rosterOnly = m.class > 0 && !m.rosterOnly
			case 2:
				m.dur = (m.dur + d + len(prepareDurations)) % len(prepareDurations)
			}
		case "a", "A":
			if m.d.Firewall.Supported && !m.d.Firewall.Ready && m.d.OpenPort != nil && !m.busy {
				m.busy = true
				open := m.d.OpenPort
				return m, func() tea.Msg { return firewallMsg(open()) }
			}
		case "enter":
			if m.blocked() || m.busy {
				return m, nil
			}
			if m.d.Firewall.Supported && !m.d.Firewall.Ready && !m.ignore {
				m.ignore = true // a second Enter carries on anyway
				return m, nil
			}
			m.Open, m.RosterOnly, m.Duration = true, m.rosterOnly, prepareDurations[m.dur]
			if m.class > 0 {
				m.Class = m.d.Classes[m.class-1]
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Prepare) keys() []keyHint {
	keys := []keyHint{{"↑↓", T("editor.ui.key_field")}, {"←→", T("editor.ui.key_option")}}
	switch {
	case m.blocked():
	case m.d.Firewall.Supported && !m.d.Firewall.Ready && !m.ignore:
		keys = append(keys, keyHint{"A", T("prepare.key.open_port")}, keyHint{"Enter", T("prepare.key.anyway")})
	default:
		keys = append(keys, keyHint{"Enter", T("prepare.key.open_room")})
	}
	return append(keys, keyHint{"Esc", T("hub.key.back")})
}

// View draws the choices on the left and the checks on the right.
func (m *Prepare) View() tea.View {
	return screenView(m.width, m.height, func(g *grid) {
		y, H := plainShell(g, T("prepare.title"), m.d.ExamTitle, m.keys())
		big := g.w >= wideCols
		w, x, b := g.w-4, 2, rSurface
		if big {
			w = 62
		}
		names := []string{T("explore.no_class")}
		for i, c := range m.d.Classes {
			names = append(names, c+" · "+strconv.Itoa(m.d.Members[i]))
		}
		durs := []string{T("prepare.exam_duration", "d", humanDuration(m.d.Duration.String()))}
		for _, d := range prepareDurations[1:] {
			durs = append(durs, T("editor.ui.minutes", "n", int(d.Minutes())))
		}
		who := []string{T("prepare.anyone"), T("prepare.list_only")}
		cur := 0
		if m.rosterOnly {
			cur = 1
		}
		checks := func(gg *grid, X, Y, c int, bg role) int {
			label(gg, X, Y, fit(T("prepare.checks"), c), bg)
			Y += 2
			if len(m.d.Problems) == 0 {
				Y = check(gg, X, Y, c, true, T("prepare.exam_ok"), bg)
			}
			for _, p := range m.d.Problems {
				Y = check(gg, X, Y, c, false, p, bg)
			}
			if len(m.d.Addrs) > 0 {
				Y = check(gg, X, Y, c, true, T("prepare.network_ok", "addr", m.d.Addrs[0]), bg)
			} else {
				Y = check(gg, X, Y, c, false, T("prepare.no_network"), bg)
			}
			if f := m.d.Firewall; f.Supported {
				text := f.Text
				if m.busy {
					text = T("prepare.opening_port")
				}
				Y = check(gg, X, Y, c, f.Ready, text, bg)
			}
			if m.class > 0 {
				n := m.d.Members[m.class-1]
				Y = check(gg, X, Y, c, n > 0, T("prepare.group_n", "n", n), bg)
			}
			return Y
		}
		sheet(g, x, y+1, w, H-3)
		body(g, x, y+1, w, H-3, 2+3*m.field, func(s *grid) int {
			fx := 16
			rows := [][]string{names, who, durs}
			labels := []string{T("prepare.group"), T("prepare.who"), T("form.manifest.duration")}
			sel := []int{m.class, cur, m.dur}
			Y := 1
			for i := range rows {
				label(s, 2, Y, fit(strings.ToUpper(labels[i]), fx-3), b)
				if i == 1 && m.class == 0 {
					Y = para(s, fx, Y, w-fx-2, T("prepare.who_needs_group"), style{fg: rInk2, bg: b}) + 2
					continue
				}
				Y = chips(s, fx, Y, rows[i], sel[i], m.field == i, b) + 2
			}
			Y = para(s, 2, Y, w-4, T("prepare.clock_hint"), style{fg: rInk2, bg: b}) + 1
			if !big {
				Y = checks(s, 4, Y+1, w-6, b)
			}
			return Y + 1
		})
		if big {
			X := x + w + 6
			checks(g, X, y+2, g.w-X-2, rBg)
		}
	})
}

// ---------------- recovering a session ----------------

// RecoverData is what happened to a session whose server stopped.
type RecoverData struct {
	ExamTitle, Code            string
	Joined, Submitted, Waiting int
	Running                    bool          // the clock had started
	Down                       time.Duration // since the server's last sign of life; 0 if unknown
	EndsAt, Now                time.Time
	Log                        []string // the server's last lines
}

// Recover reassures first, explains, and only then offers the action.
type Recover struct {
	d             *RecoverData
	width, height int
	extra         int // minutes given back to everyone
	confirmEnd    bool
	showLog       bool

	// Resume or End once the screen closes; Extra is the time to give back.
	Resume, End bool
	Extra       time.Duration
}

// NewRecover preselects the minutes the server was down, and at least
// enough to finish when the time ran out meanwhile.
func NewRecover(d *RecoverData) *Recover {
	m := &Recover{d: d, width: 120, height: 36}
	if d.Running {
		m.extra = int(math.Ceil(d.Down.Minutes()))
		m.extra = max(m.extra, m.needed())
	}
	return m
}

// needed is the extra time without which nobody could go on: the time ran out.
func (m *Recover) needed() int {
	if !m.d.Running || m.d.EndsAt.After(m.d.Now) {
		return 0
	}
	return int(math.Ceil(m.d.Now.Sub(m.d.EndsAt).Minutes())) + 5
}

// Init asks for the terminal's background, which picks the theme.
func (m *Recover) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Update adjusts the time and resumes or ends.
func (m *Recover) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case tea.KeyPressMsg:
		k := msg.String()
		if m.confirmEnd {
			m.confirmEnd = false
			if isYes(k) {
				m.End = true
				return m, tea.Quit
			}
			return m, nil
		}
		switch k {
		case "esc", "ctrl+c", "q", "Q":
			return m, tea.Quit
		case "left", "-":
			if m.d.Running {
				m.extra = max(m.needed(), m.extra-1)
			}
		case "right", "+":
			if m.d.Running {
				m.extra = min(600, m.extra+1)
			}
		case "l", "L":
			m.showLog = !m.showLog
		case "t", "T":
			m.confirmEnd = true
		case "enter":
			m.Resume, m.Extra = true, time.Duration(m.extra)*time.Minute
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Recover) keys() []keyHint {
	keys := []keyHint{{"Enter", T("recover.key.resume")}}
	if m.d.Running {
		keys = append(keys, keyHint{"←→", T("recover.key.minutes")})
	}
	return append(keys, keyHint{"T", T("recover.key.end")}, keyHint{"L", T("recover.key.log")}, keyHint{"Esc", T("hub.key.back")})
}

// View draws the screen in its fixed order: numbers, explanation, action.
func (m *Recover) View() tea.View {
	return screenView(m.width, m.height, func(g *grid) {
		d := m.d
		y, H := plainShell(g, d.ExamTitle, T("report.session", "code", d.Code), m.keys())
		big := g.w >= wideCols
		w, x, b := g.w-4, 2, rSurface
		if big && (m.showLog || len(d.Log) > 0) {
			w = 70
		}
		sheet(g, x, y+1, w, H-3)
		body(g, x, y+1, w, H-3, 0, func(s *grid) int {
			Y := para(s, 2, 1, w-4, T("recover.safe"), style{fg: rInk, bg: b, bold: true}) + 1
			down := "—"
			if d.Down > 0 {
				down = T("editor.ui.minutes", "n", int(math.Ceil(d.Down.Minutes())))
			}
			record(s, 0, Y, w, [][2]string{{strconv.Itoa(d.Joined), T("recover.joined")}, {strconv.Itoa(d.Submitted), T("recover.submitted")},
				{strconv.Itoa(d.Waiting), T("recover.waiting")}, {down, T("recover.down")}}, b)
			Y += 4
			Y = para(s, 2, Y, w-4, T("recover.answers_kept"), style{fg: rInk, bg: b}) + 1
			if !d.Running {
				Y = para(s, 2, Y, w-4, T("recover.lobby"), style{fg: rInk, bg: b}) + 1
				return Y
			}
			Y = check(s, 4, Y, w-6, d.EndsAt.After(d.Now), T("recover.clock", "time", d.EndsAt.Local().Format("15:04")), b) + 1
			label(s, 2, Y, T("recover.give_back"), b)
			v := " ‹ " + T("editor.ui.minutes", "n", m.extra) + " › "
			s.text(2, Y+2, v, style{fg: rOnInverse, bg: rInverse, bold: true})
			end := d.EndsAt.Add(time.Duration(m.extra) * time.Minute)
			para(s, 2+len([]rune(v))+2, Y+2, w-len([]rune(v))-6, T("recover.new_end", "time", end.Local().Format("15:04")), style{fg: rInk2, bg: b})
			Y += 4
			if m.needed() > 0 {
				Y = para(s, 2, Y, w-4, T("recover.ran_out"), style{fg: rInk, bg: b, bold: true}) + 1
			}
			return Y
		})
		if big && len(d.Log) > 0 {
			X := x + w + 4
			label(g, X, y+2, T("recover.log"), rBg)
			lines := d.Log
			if !m.showLog {
				lines = lines[max(0, len(lines)-3):]
			}
			Y := y + 4
			for _, l := range lines {
				if Y < y+H-1 {
					Y = para(g, X, Y, g.w-X-2, l, style{fg: rInk2, bg: rBg})
				}
			}
		} else if m.showLog {
			panel(g, 2, y+2, g.w-4, H-4, T("recover.log"))
			Y := y + 5
			for _, l := range d.Log {
				if Y < y+H-3 {
					Y = para(g, 4, Y, g.w-8, l, style{fg: rInk2, bg: rSurface})
				}
			}
		}
		if m.confirmEnd {
			confirmBox(g, T("recover.confirm_end"), "")
		}
	})
}
