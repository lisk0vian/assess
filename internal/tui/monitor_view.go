package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/session"
)

// keyItem is one key of a screen. The key bar and the ? help both come from
// this table, so they cannot disagree.
type keyItem struct {
	keyHint
	bar bool // also on the key bar
}

type keyGroup struct {
	title string
	items []keyItem
}

func (m *Monitor) keyTable() []keyGroup {
	k := func(key, label string, bar bool) keyItem { return keyItem{keyHint{key, label}, bar} }
	st := m.status()
	var who, class, sess keyGroup
	who.title, class.title, sess.title = T("monitor.ui.help_selected"), T("monitor.ui.help_class"), T("monitor.ui.help_session")
	if m.view == viewStudents {
		if r := m.selected(); r != nil {
			who.items = append(who.items, k("Enter", T("monitor.ui.key_card"), true))
			if r.AttemptID != "" {
				pause := T("monitor.key.pause")
				if r.Paused {
					pause = T("monitor.key.resume")
				}
				who.items = append(who.items, k("T", T("monitor.key.time"), true), k("M", T("monitor.key.message"), true), k("P", pause, true))
				switch r.State {
				case session.AttemptActive:
					if m.board.Lens {
						lens := T("monitor.key.lens_off")
						if r.NoLens {
							lens = T("monitor.key.lens_on")
						}
						who.items = append(who.items, k("V", lens, false))
					}
					who.items = append(who.items, k("X", T("monitor.key.annul"), true))
				case session.AttemptWithdrawn, session.AttemptAnnulled:
					who.items = append(who.items, k("U", T("monitor.key.readmit"), true))
				}
			}
			who.items = append(who.items, k("D", T("monitor.key.withdraw"), false))
			if r.Strikes > 0 {
				who.items = append(who.items, k("Z", T("monitor.key.revert_last"), false))
			}
		}
		class.items = append(class.items, k("F", T("monitor.ui.key_filter"), true), k("N", T("monitor.key.add"), false))
	}
	if m.view == viewIncidents {
		if in := m.selectedIncident(); in != nil && len(activePenalties(in)) > 0 {
			who.items = append(who.items, k("Z", T("monitor.key.revert"), true))
		}
		who.items = append(who.items, k("↑↓", T("keys.choose"), true))
	}
	if m.view == viewLink && len(m.urls) > 1 {
		who.items = append(who.items, k("←→", T("monitor.ui.key_address"), true))
	}
	if st != nil {
		switch st.State {
		case session.StateLobby:
			sess.items = append(sess.items, k("S", T("monitor.key.start"), true))
		case session.StateRunning:
			pauseAll := T("monitor.key.pause_all")
			if m.board.AllPaused {
				pauseAll = T("monitor.key.resume_all")
			}
			class.items = append(class.items, k("+", T("monitor.key.extend_all"), false), k("⇧P", pauseAll, false))
		}
		if st.State != session.StateEnded {
			class.items = append(class.items, k("⇧M", T("monitor.key.message_all"), false))
			sess.items = append(sess.items, k("E", T("monitor.key.end"), true))
		} else {
			sess.items = append(sess.items, k("I", T("monitor.key.report"), true), k("E", T("monitor.key.stop"), true))
		}
	}
	sess.items = append(sess.items, k("Tab", T("monitor.ui.key_view"), true), k("L", T("monitor.key.lock"), false),
		k("?", T("monitor.ui.key_help"), true), k("Q", T("monitor.key.quit"), false))
	return []keyGroup{who, class, sess}
}

func (m *Monitor) barKeys() []keyHint {
	var out []keyHint
	for _, g := range m.keyTable() {
		for _, it := range g.items {
			if it.bar {
				out = append(out, it.keyHint)
			}
		}
	}
	return append(out, keyHint{"Q", T("monitor.ui.key_quit")})
}

// needsAttention: strikes, gone, paused or out of the exam.
func needsAttention(r daemon.StudentRow) bool {
	return r.Strikes > 0 || r.Paused || r.State == session.AttemptAnnulled || r.State == session.AttemptWithdrawn ||
		(r.State == session.AttemptActive && !r.Online)
}

const (
	filterAll = iota
	filterAttention
	filterAnswering
	filterSubmitted
	filterOut
	filterCount
)

// stableUpTo is the class size up to which tiles never change place.
const stableUpTo = 25

func matches(filter int, r daemon.StudentRow) bool {
	switch filter {
	case filterAttention:
		return needsAttention(r)
	case filterAnswering:
		return r.State == session.AttemptActive
	case filterSubmitted:
		return r.State == session.AttemptSubmitted
	case filterOut:
		return r.AttemptID == ""
	}
	return true
}

// classRows is what the students view shows, in order: the teacher learns
// where each tile is, so only large classes bring problems to the top.
func (m *Monitor) classRows() []daemon.StudentRow {
	var out []daemon.StudentRow
	for _, r := range m.board.Students {
		if matches(m.filter, r) {
			out = append(out, r)
		}
	}
	if m.filter != filterAll || len(out) <= stableUpTo {
		return out
	}
	var first, rest []daemon.StudentRow
	for _, r := range out {
		if needsAttention(r) {
			first = append(first, r)
		} else {
			rest = append(rest, r)
		}
	}
	return append(first, rest...)
}

// density is a tile size.
type density struct {
	name            string
	w, h, gap, cols int
}

func densities(w int) []density {
	if w >= wideCols {
		return []density{{"large", 23, 4, 1, 5}, {"medium", 19, 2, 0, 6}, {"compact", 29, 1, 0, 4}}
	}
	return []density{{"medium", 19, 2, 0, 4}, {"compact", 39, 1, 0, 2}}
}

// pickDensity takes the largest tile with room for n students in h rows.
func pickDensity(w, h, n int) density {
	opts := densities(w)
	for _, d := range opts {
		if d.cols*((h+d.gap)/(d.h+d.gap)) >= n {
			return d
		}
	}
	return opts[len(opts)-1]
}

// rowState is the word for where a student is.
func (m *Monitor) rowState(r daemon.StudentRow) string {
	switch {
	case r.AttemptID == "":
		return T("monitor.state.not_joined")
	case r.State == session.AttemptActive && m.board.Status.State == session.StateLobby:
		return T("monitor.state.lobby")
	case r.Paused:
		return T("monitor.state.paused")
	case r.State == session.AttemptActive && !r.Online:
		return T("monitor.ui.offline")
	}
	return T("monitor.state." + string(r.State))
}

func (m *Monitor) tile(g *grid, x, y int, d density, r daemon.StudentRow, sel bool) {
	if d.h == 1 {
		m.tileCompact(g, x, y, d.w, r, sel)
		return
	}
	grave := r.State == session.AttemptAnnulled || r.State == session.AttemptWithdrawn
	bg, fg, fg2 := rSurface, rInk, rInk2
	switch {
	case grave:
		bg, fg, fg2 = rInverse, rOnInverse, rOnInverse2
	case r.State == session.AttemptSubmitted || r.AttemptID == "":
		bg = rSurface2
	}
	g.fill(x, y, d.w, d.h, bg)
	if sel {
		g.text(x, y, "▒", style{fg: rAccent, bg: bg})
		g.text(x+1, y, "▒", style{fg: rAccent, bg: bg})
		g.text(x+3, y, fit(r.Name, d.w-6), style{fg: rAccentInk, bg: bg, bold: true})
	} else {
		g.text(x+1, y, fit(r.Name, d.w-4), style{fg: fg, bg: bg, bold: true})
	}
	dot := "●"
	if !r.Online {
		dot = "○"
	}
	g.right(x+d.w-1, y, dot, style{fg: fg2, bg: bg})
	marks := ""
	if m.board.StrikeLimit > 0 && r.AttemptID != "" {
		marks = tally(r.Strikes, m.board.StrikeLimit)
	}
	if d.h >= 4 {
		if grave {
			g.text(x+1, y+1, fit(m.rowState(r), d.w-2), style{fg: fg, bg: bg})
			g.right(x+d.w-1, y+2, marks, style{fg: fg, bg: bg})
			return
		}
		ink := rInk
		if r.State == session.AttemptSubmitted {
			ink = rInk2
		}
		tone(g, x+1, y+1, d.w-2, r.Answered, r.Total, ink, bg)
		switch {
		case r.State == session.AttemptSubmitted:
			g.text(x+1, y+2, fit(m.rowState(r)+" "+score(r), d.w-2), style{fg: fg2, bg: bg})
		case r.State == session.AttemptActive && !r.Paused && r.Online && m.board.Status.State != session.StateLobby:
			g.text(x+1, y+2, fmt.Sprintf("P%d  %d/%d", r.Question, r.Answered, r.Total), style{fg: fg, bg: bg})
			g.right(x+d.w-1, y+2, marks, style{fg: fg, bg: bg})
		default:
			g.text(x+1, y+2, fit(m.rowState(r), d.w-len([]rune(marks))-3), style{fg: fg2, bg: bg})
			g.right(x+d.w-1, y+2, marks, style{fg: fg, bg: bg})
		}
		return
	}
	if grave {
		g.text(x+1, y+1, fit(m.rowState(r), d.w-2), style{fg: fg, bg: bg})
		return
	}
	tone(g, x+1, y+1, d.w-6, r.Answered, r.Total, rInk, bg)
	g.right(x+d.w-1, y+1, marks, style{fg: fg, bg: bg})
}

func (m *Monitor) tileCompact(g *grid, x, y, w int, r daemon.StudentRow, sel bool) {
	grave := r.State == session.AttemptAnnulled || r.State == session.AttemptWithdrawn
	bg, fg := rSurface, rInk
	switch {
	case grave:
		bg, fg = rInverse, rOnInverse
	case r.State == session.AttemptSubmitted || r.AttemptID == "":
		bg = rSurface2
	}
	g.fill(x, y, w, 1, bg)
	name := style{fg: fg, bg: bg, bold: needsAttention(r)}
	if sel {
		g.text(x, y, "▒", style{fg: rAccent, bg: bg})
		name = style{fg: rAccentInk, bg: bg, bold: true}
	}
	g.text(x+2, y, fit(r.Name, w-11), name)
	mark := strings.Repeat("■", r.Strikes)
	switch {
	case grave:
		mark = "×"
	case r.State == session.AttemptActive && !r.Online:
		mark = "○"
	}
	g.right(x+w-5, y, mark, style{fg: fg, bg: bg, bold: true})
	end := strconv.Itoa(r.Answered)
	if r.State == session.AttemptSubmitted {
		end = "✓"
	}
	g.right(x+w-1, y, end, style{fg: rInk2, bg: bg})
	if grave {
		g.right(x+w-1, y, end, style{fg: fg, bg: bg})
	}
}

// viewBar is the filter row: each view with its count.
func (m *Monitor) viewBar(g *grid, y int) {
	wide := g.w >= wideCols
	names := []string{"monitor.ui.all", "monitor.ui.attention", "monitor.ui.answering", "monitor.ui.submitted_caps", "monitor.ui.not_joined"}
	x := 1
	for i, key := range names {
		n := 0
		for _, r := range m.board.Students {
			if matches(i, r) {
				n++
			}
		}
		name := T(key)
		if !wide {
			name = T(key + "_short")
		}
		st := style{fg: rInk2, bg: rBg}
		switch {
		case i == m.filter:
			st = style{fg: rOnInverse, bg: rInverse, bold: true}
		case i == filterAttention && n > 0:
			st = style{fg: rInk, bg: rBg, bold: true}
		}
		x = g.text(x, y, fmt.Sprintf(" %s %d ", name, n), st) + 1
	}
	if wide {
		m.classPulse(g, y)
	}
}

// classPulse is a small chart of where the class has got to.
func (m *Monitor) classPulse(g *grid, y int) {
	const slots = 12
	var count [slots]int
	top := 1
	for _, r := range m.board.Students {
		if r.AttemptID == "" || r.Total == 0 {
			continue
		}
		i := min(slots-1, r.Answered*slots/r.Total)
		count[i]++
		top = max(top, count[i])
	}
	x := g.w - slots - 1
	g.right(x-1, y, T("monitor.ui.pulse"), style{fg: rInk2, bg: rBg})
	bars := []rune("▁▂▃▄▅▆▇█")
	for i, n := range count {
		if n > 0 {
			g.put(x+i, y, string(bars[max(0, n*len(bars)/top-1)]), style{fg: rInk, bg: rBg})
		}
	}
}

// classView draws the contact sheet in h rows from y.
func (m *Monitor) classView(g *grid, y, h int) {
	m.viewBar(g, y)
	y, h = y+1, h-1
	rows := m.rows()
	if len(rows) == 0 {
		text := T("monitor.ui.nobody")
		if len(m.board.Students) == 0 {
			text = T("monitor.nobody_joined")
		}
		g.text(2, y+1, fit(text, g.w-4), style{fg: rInk2, bg: rBg})
		return
	}
	x0 := 0
	if g.w >= wideCols {
		x0 = 1
		y, h = y+1, h-1
	}
	if m.filter == filterAll && len(rows) > stableUpTo {
		m.classSections(g, x0, y-(x0), h+x0, rows)
		return
	}
	d := pickDensity(g.w, h, len(rows))
	m.cols = d.cols
	lines := (h + d.gap) / (d.h + d.gap)
	top := scrollTop(m.sel/d.cols, lines, (len(rows)+d.cols-1)/d.cols) * d.cols
	for i, r := range rows[top:min(len(rows), top+lines*d.cols)] {
		m.tile(g, x0+i%d.cols*(d.w+1), y+i/d.cols*(d.h+d.gap), d, r, top+i == m.sel)
	}
}

// classSections puts what needs attention first in a large class.
func (m *Monitor) classSections(g *grid, x0, y, h int, rows []daemon.StudentRow) {
	n := 0
	for _, r := range rows {
		if needsAttention(r) {
			n++
		}
	}
	d := pickDensity(g.w, h-3, len(rows))
	m.cols = d.cols
	label(g, 2, y, T("monitor.ui.need_attention", "n", n), rBg)
	lines := min(2, (n+d.cols-1)/d.cols)
	shown := min(n, lines*d.cols)
	if n > shown {
		g.right(g.w-2, y, T("monitor.ui.more", "n", n-shown), style{fg: rInk2, bg: rBg})
	}
	y++
	for i, r := range rows[:shown] {
		m.tile(g, x0+i%d.cols*(d.w+1), y+i/d.cols*(d.h+d.gap), d, r, i == m.sel)
	}
	y += lines * (d.h + d.gap)
	if d.gap == 0 && lines > 0 {
		y++
	}
	rest := rows[n:]
	label(g, 2, y, T("monitor.ui.rest", "n", len(rest)), rBg)
	y++
	room := g.h - m.bottom - y
	lines = (room + d.gap) / (d.h + d.gap)
	sel := max(0, m.sel-n)
	top := scrollTop(sel/d.cols, lines, (len(rest)+d.cols-1)/d.cols) * d.cols
	if len(rest) > lines*d.cols {
		g.right(g.w-2, y-1, T("monitor.ui.range", "from", top+1, "to", min(len(rest), top+lines*d.cols), "total", len(rest)), style{fg: rInk2, bg: rBg})
	}
	for i, r := range rest[top:min(len(rest), top+lines*d.cols)] {
		m.tile(g, x0+i%d.cols*(d.w+1), y+i/d.cols*(d.h+d.gap), d, r, top+i+n == m.sel)
	}
}

// scrollTop keeps the line sel near the middle of a window of lines.
func scrollTop(sel, lines, total int) int {
	return max(0, min(sel-lines/2, total-lines))
}

// strip is the top band. It returns the first row below it.
func (m *Monitor) strip(g *grid, st *daemon.StatusView) int {
	b := rSurface
	soft := style{fg: rInk2, bg: b}
	bold := style{fg: rInk, bg: b, bold: true}
	strikes := 0
	if m.board != nil {
		for _, r := range m.board.Students {
			strikes += r.Strikes
		}
	}
	if g.w < wideCols {
		g.fill(0, 0, g.w, 2, b)
		g.hline(0, 2, g.w, rRuleStrong, rBg)
		g.text(1, 0, fit(st.ExamTitle, g.w-22), bold)
		g.text(1, 1, fit(T("monitor.ui.line", "code", st.Code, "online", st.Online, "joined", st.Joined, "submitted", st.Submitted, "strikes", strikes), g.w-2), soft)
		g.right(g.w-1, 0, m.stateWord(st), bold)
		return 3
	}
	g.fill(0, 0, g.w, 3, b)
	g.hline(0, 3, g.w, rRuleStrong, rBg)
	g.text(1, 0, fit(st.ExamTitle, 32), bold)
	g.text(1, 1, fit(T("monitor.ui.questions_line", "class", st.Class, "n", m.totalQuestions()), 32), soft)
	cells := [][2]string{{st.Code, T("monitor.ui.code")}, {fmt.Sprintf("%d/%d", st.Online, max(st.Joined, len(m.board.Students))), T("monitor.ui.online")}}
	if st.State == session.StateLobby {
		cells[1][1] = T("monitor.ui.in_room")
	} else {
		cells = append(cells, [2]string{strconv.Itoa(st.Active), T("monitor.ui.active")}, [2]string{strconv.Itoa(st.Submitted), T("monitor.ui.submitted")})
	}
	if m.board.StrikeLimit > 0 && st.State != session.StateLobby {
		cells = append(cells, [2]string{strconv.Itoa(strikes), T("monitor.ui.strikes")})
	}
	x := 34
	for _, c := range cells {
		g.vline(x, 0, 3, rRule, b)
		g.text(x+2, 0, c[0], bold)
		g.text(x+2, 1, c[1], soft)
		x += max(ansi.StringWidth(c[0]), ansi.StringWidth(c[1])) + 4
	}
	g.vline(x, 0, 3, rRule, b)
	switch st.State {
	case session.StateRunning:
		left := m.timeLeft(st)
		t := clock(left)
		tw := bigWidth(t) + 1
		tx := g.w - tw - 1
		g.right(tx-1, 1, T("monitor.ui.left"), soft)
		if left <= 5*time.Minute {
			g.fill(tx-1, 0, tw+2, 3, rInverse)
			bigText(g, tx, 0, t, rOnInverse, rInverse)
			if left <= time.Minute {
				for i := tx - 1; i < tx+tw+1; i++ {
					g.put(i, 3, "▀", style{fg: rAccent, bg: rBg})
				}
			}
		} else {
			bigText(g, tx, 0, t, rInk, b)
		}
	default:
		g.right(g.w-2, 0, m.stateWord(st), bold)
	}
	return 4
}

func (m *Monitor) timeLeft(st *daemon.StatusView) time.Duration {
	now := time.Now().Add(m.offset).UnixMilli()
	return max(time.Duration(st.EndsAt-now)*time.Millisecond, 0)
}

func (m *Monitor) stateWord(st *daemon.StatusView) string {
	switch st.State {
	case session.StateLobby:
		return T("monitor.lobby")
	case session.StateRunning:
		return T("monitor.ui.left_short", "time", clock(m.timeLeft(st)))
	}
	return T("monitor.ended_state")
}

func (m *Monitor) totalQuestions() int {
	for _, r := range m.board.Students {
		if r.Total > 0 {
			return r.Total
		}
	}
	return 0
}

// requestBand shows what waits for the teacher's decision.
func (m *Monitor) requestBand(g *grid, y int) int {
	r := m.firstRequest()
	if r == nil {
		return 0
	}
	what := T("monitor.request.device")
	switch r.Kind {
	case "late":
		what = T("monitor.request.late")
	case "unlisted":
		what = T("monitor.request.unlisted", "class", m.board.Status.Class)
	}
	b := rSurface2
	g.fill(0, y, g.w, 1, b)
	g.text(0, y, "▒▒", style{fg: rAccent, bg: b})
	text := fmt.Sprintf("%s (%s) %s", r.Name, r.StudentID, what)
	if n := len(m.board.Requests); n > 1 {
		text += " " + T("monitor.request.more", "n", n-1)
	}
	keys := []keyHint{{"A", T("monitor.key.approve")}, {"R", T("monitor.key.reject")}}
	if m.board.Status.Class != "" {
		keys = []keyHint{{"A", T("monitor.key.approve")}, {"G", T("monitor.key.enroll", "class", m.board.Status.Class)}, {"R", T("monitor.key.reject")}}
	}
	room := g.w - 3
	for _, k := range keys {
		room -= keyWidth(k) + 1
	}
	x := g.text(3, y, fit(text, room), style{fg: rInk, bg: b, bold: true}) + 2
	for _, k := range keys {
		x = g.text(x, y, " "+k.key+" ", style{fg: rOnInverse, bg: rInverse, bold: true})
		x = g.text(x, y, " "+k.label, style{fg: rInk2, bg: b}) + 2
	}
	return 1
}

func (m *Monitor) incidentsView(g *grid, y, h int) {
	b := rSurface
	g.fill(0, y, g.w, h, b)
	y++
	if len(m.board.AwayHits) > 0 {
		section(g, 2, y, T("monitor.away.title"), T("monitor.away.hint"), b)
		y++
		for _, a := range m.board.AwayHits {
			hard := T("monitor.away.difficulty", "pct", int(a.Difficulty*100+0.5))
			line := T("monitor.away.line", "signal", signalName(a.Signal), "away", a.AwayMs/1000, "after", a.AfterMs/1000, "difficulty", hard)
			g.text(2, y, fit("↩ "+pad(a.Name, 22)+" "+pad(a.QuestionID, 8)+" "+line, g.w-4), style{fg: rInk, bg: b, bold: a.Strong})
			y++
		}
		y++
	}
	if len(m.board.Incidents) == 0 {
		g.text(2, y, T("monitor.no_incidents"), style{fg: rInk2, bg: b})
		if m.board.StrikeLimit == 0 {
			g.text(2, y+1, T("monitor.proctoring_off"), style{fg: rInk2, bg: b})
		}
		return
	}
	head := pad(T("monitor.col.time"), 9) + pad(T("monitor.col.student"), 23) + pad(T("monitor.col.signal"), 29) + pad(T("monitor.col.question"), 7) + T("monitor.col.effect")
	label(g, 2, y, fit(head, g.w-3), b)
	g.hline(0, y+1, g.w, rRuleStrong, b)
	y += 2
	room := h - y + 4
	lines := max(1, min(room, g.h-m.bottom-y))
	top := scrollTop(m.sel, lines, len(m.board.Incidents))
	for i, in := range m.board.Incidents[top:min(len(m.board.Incidents), top+lines)] {
		m.selectable(g, y+i, top+i == m.sel, incidentLine(in), b)
	}
}

// selectable draws one row of a list with the cursor.
func (m *Monitor) selectable(g *grid, y int, on bool, text string, b role) {
	bg, fg := b, rInk
	if on {
		bg, fg = rSurface2, rAccentInk
		g.fill(0, y, g.w, 1, bg)
		g.text(0, y, "▒", style{fg: rAccent, bg: bg})
	}
	g.text(2, y, fit(text, g.w-3), style{fg: fg, bg: bg, bold: on})
}

func (m *Monitor) rankingView(g *grid, y, h int) {
	b := rSurface
	g.fill(0, y, g.w, h, b)
	rows := m.rows()
	if len(rows) == 0 {
		g.text(2, y+1, T("monitor.nobody_submitted"), style{fg: rInk2, bg: b})
		return
	}
	y++
	lines := max(1, g.h-m.bottom-y)
	top := scrollTop(m.sel, lines, len(rows))
	for i, r := range rows[top:min(len(rows), top+lines)] {
		line := fmt.Sprintf("%2d. %s %8s   %s", top+i+1, pad(r.Name, 28), score(r), clock(time.Duration(r.UsedMs)*time.Millisecond))
		m.selectable(g, y+i, top+i == m.sel, line, b)
	}
}

// linkView shows how students get in.
func (m *Monitor) linkView(g *grid, y, h int, st *daemon.StatusView) {
	b := rSurface
	g.fill(0, y, g.w, h, b)
	qw, qh := qrSize(m.qr)
	tx := 2
	if qh+2 <= h && g.w >= qw+36 {
		qrBlock(g, 2, y+1, m.qr)
		tx = qw + 5
	}
	label(g, tx, y+1, T("monitor.ui.code_caps"), b)
	codeCells(g, tx, y+2, st.Code, b)
	ly := y + 6
	label(g, tx, ly, T("monitor.ui.address"), b)
	ly++
	for i, u := range m.urls {
		if i == m.urlIdx {
			g.text(tx, ly, "› ", style{fg: rAccentInk, bg: b, bold: true})
			g.text(tx+2, ly, fit(u, g.w-tx-3), style{fg: rInk, bg: b, bold: true})
		} else {
			g.text(tx+2, ly, fit(u, g.w-tx-3), style{fg: rInk2, bg: b})
		}
		ly++
	}
	if len(m.urls) > 1 {
		g.text(tx, ly+1, fit(T("monitor.other_address"), g.w-tx-2), style{fg: rInk2, bg: b})
	}
}

// lobbyView is the room: who has joined, and how to join.
func (m *Monitor) lobbyView(g *grid, y, h int, st *daemon.StatusView) {
	qw, qh := qrSize(m.qr)
	if g.w < wideCols || qh+6 > h {
		m.linkView(g, y, h, st)
		return
	}
	pw := qw + 6
	px := g.w - pw - 1
	const tw, th, gap = 19, 2, 1
	cols := max(1, (px-2)/(tw+1))
	lines := (h - 1 + gap) / (th + gap)
	rows := m.board.Students
	top := scrollTop(m.sel/cols, lines, (len(rows)+cols-1)/cols) * cols
	for i, r := range rows[top:min(len(rows), top+lines*cols)] {
		x, ty := 1+i%cols*(tw+1), y+1+i/cols*(th+gap)
		bg, fg := rSurface, rInk
		if r.AttemptID == "" {
			bg, fg = rSurface2, rInk2
		}
		g.fill(x, ty, tw, th, bg)
		g.text(x+1, ty, fit(r.Name, tw-2), style{fg: fg, bg: bg, bold: true})
		g.text(x+1, ty+1, fit(m.rowState(r), tw-2), style{fg: rInk2, bg: bg})
	}
	if len(rows) == 0 {
		g.text(2, y+1, fit(T("monitor.nobody_joined"), px-4), style{fg: rInk2, bg: rBg})
	}
	g.fill(px, y+1, pw, h-1, rSurface)
	qrBlock(g, px+3, y+2, m.qr)
	ty := y + 3 + qh
	b := rSurface
	g.text(g.text(px+3, ty, T("monitor.ui.code_word")+" ", style{fg: rInk2, bg: b}), ty, st.Code, style{fg: rInk, bg: b, bold: true})
	g.text(px+3, ty+1, fit(m.url(), pw-4), style{fg: rInk, bg: b, bold: true})
	if len(m.urls) > 1 {
		g.text(px+3, ty+2, fit(T("monitor.ui.other_addresses", "n", len(m.urls)-1), pw-4), style{fg: rInk2, bg: b})
	}
}

// card is the student's side panel: the class stays in view.
func (m *Monitor) card(g *grid) {
	r := m.selected()
	if r == nil {
		m.showCard = false
		return
	}
	w := min(54, g.w-4)
	x, y := g.w-w-1, 4
	if g.w < wideCols {
		x, y = g.w-w-2, 3
	}
	h := g.h - y - m.bottom
	b := rSurface
	panel(g, x, y, w, h, r.Name)
	pos := r.StudentID + " · " + m.rowState(*r)
	if r.State == session.AttemptActive && r.Question > 0 {
		pos = r.StudentID + " · " + T("monitor.ui.card_question", "q", r.Question, "n", r.Total)
	}
	g.text(x+2, y+2, fit(pos, w-4), style{fg: rInk2, bg: b})
	online := T("common.no_short")
	if r.Online {
		online = T("common.yes_short")
	}
	strikes := "—"
	if m.board.StrikeLimit > 0 {
		strikes = tally(r.Strikes, m.board.StrikeLimit)
	}
	record(g, x, y+3, w, [][2]string{{fmt.Sprintf("%d/%d", r.Answered, r.Total), T("monitor.ui.card_answers")},
		{strikes, T("monitor.ui.strikes")}, {strconv.Itoa(r.Signals), T("monitor.ui.card_signals")}, {online, T("monitor.ui.online")}}, b)
	ly := y + 7
	label(g, x+2, ly, T("monitor.ui.card_what"), b)
	ly++
	var own []daemon.Incident
	for _, in := range m.board.Incidents {
		if in.AttemptID == r.AttemptID {
			own = append(own, in)
		}
	}
	if len(own) == 0 {
		g.text(x+2, ly, T("monitor.ui.card_none"), style{fg: rInk2, bg: b})
	}
	room := max(0, (h-14)/2)
	for _, in := range own[:min(len(own), room)] {
		what := signalName(in.Kind)
		if in.DurationMs > 0 {
			what += fmt.Sprintf(" %ds", in.DurationMs/1000)
		}
		g.text(x+2, ly, time.UnixMilli(in.At).Format("15:04")+"  "+fit(what, w-10), style{fg: rInk, bg: b})
		g.text(x+9, ly+1, fit(penaltyLine(in), w-11), style{fg: rInk2, bg: b})
		ly += 2
	}
	var notes []string
	if r.ExtraMs > 0 {
		notes = append(notes, fmt.Sprintf("+%d min", r.ExtraMs/60000))
	}
	if r.NoLens {
		notes = append(notes, T("monitor.no_lens"))
	}
	if r.AwayHits > 0 {
		notes = append(notes, T("report.n.away", "n", r.AwayHits))
	}
	g.text(x+2, y+h-2, fit(strings.Join(notes, " · "), w-4), style{fg: rInk2, bg: b})
}

func penaltyLine(in daemon.Incident) string {
	var fx []string
	for _, p := range in.Penalties {
		s := penaltyName(string(p.Kind))
		if p.Kind == session.PenaltyTime {
			s = fmt.Sprintf("−%d min", int(p.Amount)/60000)
		}
		if !p.Active() {
			s += " " + T("monitor.reverted_mark")
		}
		fx = append(fx, s)
	}
	if len(fx) == 0 {
		return T("severity." + string(in.Severity))
	}
	return strings.Join(fx, ", ")
}

func (m *Monitor) promptPanel(g *grid) {
	w := min(66, g.w-4)
	x, y := (g.w-w)/2, max(4, g.h/2-5)
	b := rSurface
	panel(g, x, y, w, 7, fit(m.question, w-4))
	value := m.input.Value()
	if value == "" {
		g.fill(x+2, y+3, w-4, 1, rSurface2)
		g.text(x+3, y+3, fit(m.input.Placeholder, w-6), style{fg: rInk3, bg: rSurface2})
		g.put(x+3, y+3, "▌", style{fg: rAccent, bg: rSurface2})
	} else {
		inputLine(g, x+2, y+3, w-4, value, rSurface2)
	}
	g.text(x+2, y+5, T("monitor.prompt_hint"), style{fg: rInk2, bg: b})
}

func (m *Monitor) confirmPlate(g *grid) {
	alt := ""
	if m.onAlt != nil {
		alt = m.altLabel
	}
	confirmBox(g, m.question, alt)
}

func (m *Monitor) lockedView(g *grid, st *daemon.StatusView) {
	b := rSurface
	g.fill(0, 0, g.w, 2, b)
	g.hline(0, 2, g.w, rRuleStrong, rBg)
	g.text(1, 0, "Assess", style{fg: rInk, bg: b, bold: true})
	g.right(g.w-2, 0, m.stateWord(st), style{fg: rInk, bg: b, bold: true})
	m.bottom = keyBar(g, []keyHint{{"Enter", T("monitor.ui.key_unlock")}, {"Ctrl+C", T("monitor.key.quit")}})
	w := min(64, g.w-8)
	x, y := (g.w-w)/2, max(4, g.h/2-8)
	sheet(g, x, y, w, 14)
	g.text(x+3, y+2, fit(T("monitor.locked"), w-6), style{fg: rInk, bg: b, bold: true})
	record(g, x, y+4, w, [][2]string{{strconv.Itoa(st.Online), T("monitor.ui.online")}, {strconv.Itoa(st.Submitted), T("monitor.ui.submitted")}}, b)
	label(g, x+3, y+8, fit(T("monitor.unlock_prompt"), w-6), b)
	g.fill(x+3, y+9, w-6, 1, rSurface2)
	inputLine(g, x+3, y+9, w-6, strings.Repeat("*", len([]rune(m.input.Value()))), rSurface2)
	if m.notice != "" {
		g.fill(x, y+11, w, 2, rInverse)
		g.text(x+3, y+11, fit(m.notice, w-6), style{fg: rOnInverse, bg: rInverse, bold: true})
	} else {
		g.text(x+3, y+11, fit(T("monitor.ui.no_names"), w-6), style{fg: rInk2, bg: b})
	}
}

// View draws the panel on a grid the size of the window.
func (m *Monitor) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	g := newGrid(w, h, rBg)
	st := m.status()
	switch {
	case w < 72 || h < 16:
		for i, l := range wrap(T("monitor.ui.too_small"), max(10, w-2)) {
			g.text(1, 1+i, l, style{fg: rInk, bg: rBg})
		}
	case st == nil:
		m.connecting(g)
	case m.mode == modeLocked:
		m.lockedView(g, st)
	default:
		m.panelView(g, st)
	}
	v := tea.NewView(g.String())
	v.AltScreen = true
	v.WindowTitle = "Assess"
	return v
}

func (m *Monitor) connecting(g *grid) {
	g.fill(0, 0, g.w, 2, rSurface)
	g.hline(0, 2, g.w, rRuleStrong, rBg)
	g.text(1, 0, "Assess", style{fg: rInk, bg: rSurface, bold: true})
	if m.err != nil {
		plate(g, 2, 4, g.w-4, T("monitor.no_connection"), m.err.Error())
		return
	}
	g.text(2, 4, T("monitor.connecting"), style{fg: rInk2, bg: rBg})
}

func (m *Monitor) panelView(g *grid, st *daemon.StatusView) {
	y := m.strip(g, st)
	m.bottom = keyBar(g, m.barKeys()) + 1
	names := make([]string, viewCount)
	for i, key := range viewKeys {
		names[i] = strings.ToUpper(T(key))
	}
	if n := len(m.board.Incidents); n > 0 {
		names[viewIncidents] += " " + strconv.Itoa(n)
	}
	tabBar(g, y, names, m.view)
	y++
	if st.LastError != "" {
		g.fill(0, y, g.w, 1, rInverse)
		g.text(1, y, fit(T("monitor.server_error")+" "+st.LastError, g.w-2), style{fg: rOnInverse, bg: rInverse, bold: true})
		y++
	}
	y += m.requestBand(g, y)
	h := g.h - m.bottom - y
	switch m.view {
	case viewLink:
		if st.State == session.StateLobby {
			m.lobbyView(g, y, h, st)
		} else {
			m.linkView(g, y, h, st)
		}
	case viewIncidents:
		m.incidentsView(g, y, h)
	case viewRanking:
		m.rankingView(g, y, h)
	default:
		m.classView(g, y, h)
	}
	line := g.h - m.bottom
	switch {
	case m.err != nil:
		g.fill(0, line, g.w, 1, rInverse)
		g.text(1, line, fit(T("monitor.warning")+" "+m.err.Error(), g.w-2), style{fg: rOnInverse, bg: rInverse, bold: true})
	case m.noOneYet(st):
		g.fill(0, line, g.w, 1, rInverse)
		g.text(1, line, fit(T("monitor.nobody_hint"), g.w-2), style{fg: rOnInverse, bg: rInverse, bold: true})
	case m.notice != "":
		g.fill(0, line, g.w, 1, rBg)
		g.text(1, line, fit("✓ "+m.notice, g.w-2), style{fg: rInk, bg: rBg, bold: true})
	}
	if m.showCard {
		m.card(g)
	}
	switch {
	case m.showHelp:
		helpPanel(g, m.keyTable())
	case m.mode == modePrompt:
		m.promptPanel(g)
	case m.mode == modeConfirm:
		m.confirmPlate(g)
	}
}
