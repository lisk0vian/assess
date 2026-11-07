package tui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/report"
)

// The report as the design draws it: first what to look at, then the proof.
// Each view is drawn on a grid as tall as it needs and shown through a
// window, so short terminals scroll instead of needing another layout.

const repBodyRows = 400

func (m *Report) View() tea.View {
	w, h := max(1, m.width), max(1, m.height)
	g := newGrid(w, h, rBg)
	y := m.repStrip(g)
	names := make([]string, repCount)
	for i, key := range repKeys {
		names[i] = strconv.Itoa(i+1) + " " + strings.ToUpper(T(key))
	}
	tabBar(g, y, names, m.view)
	y++
	body, used := m.drawBody(m.view, w)
	keys := []keyHint{{"←→", T("report.key.view")}, {"1-5", T("report.key.goto")}}
	switch m.view {
	case repQuestions:
		keys = append(keys, keyHint{"↑↓", T("report.ui.key_question")}, keyHint{"O", T("report.ui.key_sort")})
	case repEvidence:
		keys = append(keys, keyHint{"↑↓", T("report.ui.key_pair")})
	}
	rows := keyRows(w, append(slices.Clone(keys), keyHint{"↑↓", T("report.key.scroll", "from", 100, "to", 100, "total", 100)}, keyHint{"Q", T("editor.key.quit")}))
	room := max(1, h-y-rows)
	m.scroll = min(max(0, m.scroll), max(0, used-room))
	if used > room && m.view != repQuestions && m.view != repEvidence {
		keys = append(keys, keyHint{"↑↓", T("report.key.scroll", "from", m.scroll+1, "to", min(used, m.scroll+room), "total", used)})
	}
	keyBar(g, append(keys, keyHint{"Q", T("editor.key.quit")}))
	g.fill(0, y, w, room, rSurface)
	for j := range room {
		copy(g.c[(y+j)*w:(y+j+1)*w], body.c[(m.scroll+j)*w:(m.scroll+j+1)*w])
	}
	v := tea.NewView(g.String())
	v.AltScreen = true
	v.WindowTitle = "Assess"
	return v
}

func (m *Report) repStrip(g *grid) int {
	b := rSurface
	s, sum := m.r.Session, m.r.Summary
	soft := style{fg: rInk2, bg: b}
	bold := style{fg: rInk, bg: b, bold: true}
	sub := T("report.session", "code", s.Code) + " · " + span(s.StartedAt, s.EndedAt)
	if g.w < wideCols {
		g.fill(0, 0, g.w, 2, b)
		g.hline(0, 2, g.w, rRuleStrong, rBg)
		g.text(1, 0, fit(s.ExamTitle, g.w-2), bold)
		g.text(1, 1, fit(sub, g.w-2), soft)
		return 3
	}
	g.fill(0, 0, g.w, 3, b)
	g.hline(0, 3, g.w, rRuleStrong, rBg)
	g.text(1, 0, fit(s.ExamTitle, 38), bold)
	g.text(1, 1, fit(sub, 38), soft)
	x := 40
	for _, c := range [][2]string{{fmt.Sprintf("%d/%d", sum.Submitted, sum.Students), T("report.ui.submitted")},
		{strconv.Itoa(sum.Annulled), T("report.ui.annulled")}, {strconv.Itoa(sum.Withdrawn), T("report.ui.withdrawn")}} {
		g.vline(x, 0, 3, rRule, b)
		g.text(x+2, 0, c[0], bold)
		g.text(x+2, 1, c[1], soft)
		x += max(ansi.StringWidth(c[0]), ansi.StringWidth(c[1])) + 4
	}
	g.vline(x, 0, 3, rRule, b)
	return 4
}

// drawBody draws a view and returns its grid and the rows it used.
func (m *Report) drawBody(view, w int) (*grid, int) {
	g := newGrid(w, repBodyRows, rSurface)
	y := 1
	switch view {
	case repQuestions:
		y = m.drawQuestions(g, y)
	case repIncidents:
		y = m.drawIncidents(g, y)
	case repEvidence:
		y = m.drawEvidence(g, y)
	case repHistory:
		y = m.drawHistory(g, y)
	default:
		y = m.drawSummary(g, y)
	}
	return g, min(y+1, repBodyRows)
}

func bigPercent(g *grid, x, y int, v float64, name string, b role) int {
	e := bigText(g, x, y, strconv.Itoa(int(math.Round(v))), rInk, b)
	g.text(e, y+1, "%", style{fg: rInk, bg: b, bold: true})
	g.text(e, y+2, name, style{fg: rInk2, bg: b})
	return e + ansi.StringWidth(name) + 3
}

// findings says what to look at first. It only states what the data backs.
func (m *Report) findings() []keyHint {
	var out []keyHint
	var worst *report.QuestionStat
	for i := range m.r.Questions {
		q := &m.r.Questions[i]
		if q.Seen > 0 && (worst == nil || q.Rate < worst.Rate) {
			worst = q
		}
	}
	if worst != nil && worst.Rate < 0.5 {
		out = append(out, keyHint{"2", T("report.ui.first_hard", "q", worst.ID, "pct", int(math.Round((1-worst.Rate)*100)))})
	}
	if n := len(m.r.Pairs); n > 0 {
		out = append(out, keyHint{"4", T("report.n.pairs", "n", n)})
	}
	if i := slices.IndexFunc(m.r.History, func(h report.SessionLine) bool { return h.Current }); i > 0 {
		d := m.r.History[i].Mean - m.r.History[i-1].Mean
		if math.Abs(d) >= 1 {
			key := "report.ui.first_up"
			if d < 0 {
				key = "report.ui.first_down"
			}
			out = append(out, keyHint{"5", T(key, "n", int(math.Round(math.Abs(d))))})
		}
	}
	return out
}

func (m *Report) drawSummary(g *grid, y int) int {
	b := rSurface
	s := m.r.Summary
	wide := g.w >= wideCols
	if s.Submitted == 0 {
		g.text(2, y, T("report.nobody_submitted"), style{fg: rInk2, bg: b})
		return y + 1
	}
	stats := [][2]string{{pct(s.Min) + " – " + pct(s.Max), T("report.ui.range")}, {"±" + num(s.StdDev), T("report.ui.sd")}, {minutes(s.MedianMinutes), T("report.ui.time")}}
	if wide {
		x := bigPercent(g, 2, y, s.Mean, T("report.ui.mean"), b)
		x = bigPercent(g, x, y, s.Median, T("report.ui.median"), b)
		for _, c := range stats {
			g.text(x, y, c[0], style{fg: rInk, bg: b, bold: true})
			g.text(x, y+1, c[1], style{fg: rInk2, bg: b})
			x += max(ansi.StringWidth(c[0]), ansi.StringWidth(c[1])) + 3
		}
		if f := m.findings(); len(f) > 0 {
			fx := g.w - 44
			fx = max(x+1, fx)
			g.fill(fx, y, g.w-fx-1, len(f)+1, rSurface2)
			g.text(fx+2, y, T("report.ui.first"), style{fg: rInk, bg: rSurface2, bold: true})
			for i, k := range f {
				g.text(fx+2, y+1+i, fit(k.label, g.w-fx-9), style{fg: rInk, bg: rSurface2})
				g.text(g.w-6, y+1+i, " "+k.key+" ", style{fg: rOnInverse, bg: rInverse, bold: true})
			}
		}
		y += 5
	} else {
		x := g.text(2, y, pct(s.Mean)+" "+T("report.ui.mean"), style{fg: rInk, bg: b, bold: true})
		x = g.text(x+2, y, pct(s.Median)+" "+T("report.ui.median"), style{fg: rInk2, bg: b})
		g.text(x+2, y, "±"+num(s.StdDev)+"  "+minutes(s.MedianMinutes), style{fg: rInk2, bg: b})
		y += 2
		for _, k := range m.findings() {
			g.text(2, y, "▒▒", style{fg: rAccent, bg: b})
			g.text(5, y, fit(k.label, g.w-7), style{fg: rInk, bg: b, bold: true})
			y++
		}
		if len(m.findings()) > 0 {
			y++
		}
	}
	section(g, 2, y, T("report.scores.title"), T("report.ui.unit_hint"), b)
	end := unitChart(g, 2, y+2, min(54, g.w-4), m.r.Scores, s.Median, b)
	timed := slices.ContainsFunc(m.r.Students, func(st report.StudentLine) bool { return st.Percent != nil && st.Minutes > 0 })
	if wide && g.w >= 110 && timed {
		section(g, 60, y, T("report.ui.scatter_title"), T("report.ui.scatter_hint"), b)
		end = max(end, m.scatter(g, 60, y+2, g.w-62, 8, b))
	} else {
		section(g, 2, end, T("report.times.title"), T("report.times.hint"), b)
		end = unitChart(g, 2, end+2, min(54, g.w-4), m.r.Times, -1, b)
	}
	y = end + 1
	section(g, 2, y, T("report.review.title"), T("report.review.hint"), b)
	y++
	label(g, 2, y, pad(T("monitor.col.student"), 28)+pad(T("monitor.col.state"), 14)+pad(T("monitor.col.score"), 8)+T("report.ui.reason"), b)
	y++
	g.hline(0, y, g.w, rRuleStrong, b)
	y++
	n := 0
	for _, st := range m.r.Students {
		if st.Strikes == 0 && st.AwayHits == 0 && st.Pairs == 0 && st.State != "annulled" {
			continue
		}
		n++
		var why []string
		if st.Strikes > 0 {
			why = append(why, T("report.n.strikes", "n", st.Strikes))
		}
		if st.Pairs > 0 {
			why = append(why, T("report.n.pairs", "n", st.Pairs))
		}
		if st.AwayHits > 0 {
			why = append(why, T("report.n.away", "n", st.AwayHits))
		}
		grade := "—"
		if st.Percent != nil {
			grade = pct(*st.Percent)
		}
		line := pad(st.Name, 28) + pad(stateName(st.State), 14) + pad(grade, 8) + strings.Join(why, " · ")
		if st.State == "annulled" {
			g.fill(0, y, g.w, 1, rInverse)
			g.text(2, y, fit(line, g.w-3), style{fg: rOnInverse, bg: rInverse, bold: true})
		} else {
			g.text(2, y, fit(line, g.w-3), style{fg: rInk, bg: b})
		}
		y++
	}
	if n == 0 {
		g.text(2, y, T("report.review.none"), style{fg: rInk2, bg: b})
		y++
	}
	return y
}

// unitChart stacks one half-cell block per student, with the count on top.
// When a band has more students than rows, each block stands for several
// and the chart says so. It returns the row after the chart.
func unitChart(g *grid, x, y, w int, bins []report.Bin, median float64, b role) int {
	const rows = 6
	if len(bins) == 0 {
		return y
	}
	top := 1
	for _, bin := range bins {
		top = max(top, bin.Count)
	}
	each := (top + rows - 1) / rows
	cw := max(3, min(5, w/len(bins)))
	for i, bin := range bins {
		cx := x + i*cw
		n := (bin.Count + each - 1) / each
		for j := range n {
			g.text(cx, y+rows-j, strings.Repeat("▄", cw-1), style{fg: rInk, bg: b})
		}
		if bin.Count > 0 {
			g.text(cx+1, y+rows-n, strconv.Itoa(bin.Count), style{fg: rInk, bg: b, bold: true})
		}
		if cw >= 4 || i%2 == 0 {
			g.text(cx, y+rows+2, strconv.FormatFloat(bin.From, 'f', -1, 64), style{fg: rInk2, bg: b})
		}
	}
	g.hline(x, y+rows+1, len(bins)*cw, rRuleStrong, b)
	next := y + rows + 3
	if median >= 0 {
		lo, hi := bins[0].From, bins[len(bins)-1].To
		mx := x + int((median-lo)/(hi-lo)*float64(len(bins)*cw))
		g.text(min(mx, g.w-20), next, "↑ "+T("report.ui.median")+" "+pct(median), style{fg: rInk, bg: b, bold: true})
		next++
	}
	if each > 1 {
		g.text(x, next, T("report.ui.each_block", "n", each), style{fg: rInk2, bg: b})
		next++
	}
	return next + 1
}

// scatter sets each student's grade against the time they used, with the
// spread of times under the axis.
func (m *Report) scatter(g *grid, x, y, w, h int, b role) int {
	type dot struct{ min, pct float64 }
	var dots []dot
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, st := range m.r.Students {
		if st.Percent != nil && st.Minutes > 0 {
			dots = append(dots, dot{st.Minutes, *st.Percent})
			lo, hi = math.Min(lo, st.Minutes), math.Max(hi, st.Minutes)
		}
	}
	if len(dots) == 0 {
		return y
	}
	if hi <= lo {
		hi = lo + 1
	}
	px, pw := x+5, w-6
	g.vline(px-1, y, h, rRuleStrong, b)
	g.text(x, y, "100", style{fg: rInk2, bg: b})
	g.text(x+2, y+h-1, "0", style{fg: rInk2, bg: b})
	g.hline(px-1, y+h, pw+1, rRuleStrong, b)
	seen := map[[2]int]int{}
	margin := make([]int, pw)
	for _, d := range dots {
		cx := int((d.min - lo) / (hi - lo) * float64(pw-1))
		cy := int((100 - d.pct) / 100 * float64(h-1))
		margin[cx]++
		seen[[2]int{cx, cy}]++
		glyph, bold := "●", false
		if n := seen[[2]int{cx, cy}]; n > 1 {
			glyph, bold = strconv.Itoa(min(n, 9)), true
		}
		g.put(px+cx, y+cy, glyph, style{fg: rInk, bg: b, bold: bold})
	}
	top := slices.Max(margin)
	bars := []rune("▁▂▃▄▅▆▇█")
	for i, n := range margin {
		if n > 0 {
			g.put(px+i, y+h+1, string(bars[max(0, n*len(bars)/top-1)]), style{fg: rInk2, bg: b})
		}
	}
	g.text(px, y+h+2, minutes(math.Round(lo)), style{fg: rInk2, bg: b})
	g.right(px+pw, y+h+2, minutes(math.Round(hi)), style{fg: rInk2, bg: b})
	return y + h + 4
}

// orderedQuestions applies the sort of the Questions view.
func (m *Report) orderedQuestions() []report.QuestionStat {
	qs := slices.Clone(m.r.Questions)
	if m.byRate {
		slices.SortStableFunc(qs, func(a, b report.QuestionStat) int {
			switch {
			case a.Rate < b.Rate:
				return -1
			case a.Rate > b.Rate:
				return 1
			}
			return 0
		})
	}
	return qs
}

func flagged(q report.QuestionStat) bool {
	return q.Leak || q.AwayHits > 0 || (q.Discrimination != nil && *q.Discrimination < 0.15 && q.Rate > 0 && q.Rate < 1)
}

func questionNotes(q report.QuestionStat) []string {
	var notes []string
	switch {
	case q.Seen == 0:
		notes = append(notes, T("report.note.unseen"))
	case q.Discrimination != nil && *q.Discrimination < 0:
		notes = append(notes, T("report.note.negative"))
	case q.Discrimination != nil && *q.Discrimination < 0.15 && q.Rate > 0 && q.Rate < 1:
		notes = append(notes, T("report.note.weak"))
	}
	if q.Leak {
		notes = append(notes, T("report.note.leak"))
	}
	if q.AwayHits > 0 {
		notes = append(notes, T("report.note.away", "n", q.AwayHits))
	}
	if q.Voided > 0 {
		notes = append(notes, T("report.note.voided", "students", T("report.n.students", "n", q.Voided)))
	}
	return notes
}

func (m *Report) drawQuestions(g *grid, y int) int {
	b := rSurface
	qs := m.orderedQuestions()
	wide := g.w >= wideCols
	section(g, 2, y, T("report.rates.title"), T("report.rates.hint"), b)
	y += 2
	tw := g.w
	if wide {
		tw = min(74, g.w*3/5)
	}
	if len(qs) == 0 {
		return y
	}
	m.sel = min(max(0, m.sel), len(qs)-1)
	bw := max(8, tw-48)
	if !wide {
		bw = max(8, tw-34)
	}
	head := pad(T("monitor.col.question"), 7)
	if wide {
		head += pad(T("editor.col.type"), 11)
	}
	head += pad(T("report.col.right"), bw+6) + pad(T("report.col.blank"), 10) + T("report.col.discrimination")
	label(g, 2, y, fit(head, tw-3), b)
	g.hline(0, y+1, tw, rRuleStrong, b)
	top := y
	y += 2
	first := y
	for i, q := range qs {
		bg, fg := b, rInk
		if i == m.sel {
			bg, fg = rSurface2, rAccentInk
			g.fill(0, y, tw, 1, bg)
			g.text(0, y, "▒", style{fg: rAccent, bg: bg})
		}
		x := g.text(2, y, pad(q.ID, 6), style{fg: fg, bg: bg, bold: true}) + 1
		if wide {
			x = g.text(x, y, pad(typeName(q.Type), 10), style{fg: rInk2, bg: bg}) + 1
		}
		if q.Seen > 0 {
			halfBar(g, x, y, bw, q.Rate, rInk, bg)
			g.right(x+bw+5, y, pct(q.Rate*100), style{fg: rInk, bg: bg, bold: true})
			blank := pct(float64(q.Seen-q.Answered) / float64(q.Seen) * 100)
			g.right(x+bw+13, y, blank, style{fg: rInk2, bg: bg})
		}
		if q.Discrimination != nil {
			st := style{fg: rInk2, bg: bg}
			if *q.Discrimination < 0.15 {
				st = style{fg: rInk, bg: bg, bold: true}
			}
			g.right(min(tw-1, x+bw+22), y, num(*q.Discrimination), st)
		}
		y++
	}
	end := y + 1
	q := qs[m.sel]
	if wide {
		x := tw + 2
		w := g.w - x - 2
		g.vline(tw, top, max(end-top, 24), rRule, b)
		section(g, x, top, T("report.ui.quadrant_title"), "", b)
		m.quadrant(g, x, top+1, w, 12, q)
		dy := m.questionDetail(g, x, top+15, w, q)
		return max(end, dy)
	}
	m.keepVisible(first + m.sel)
	return m.questionDetail(g, 2, end, g.w-4, q)
}

// keepVisible scrolls so the row of the cursor stays on screen.
func (m *Report) keepVisible(row int) {
	room := max(1, m.height-8)
	switch {
	case row < m.scroll:
		m.scroll = row
	case row >= m.scroll+room:
		m.scroll = row - room + 1
	}
}

func (m *Report) questionDetail(g *grid, x, y, w int, q report.QuestionStat) int {
	b := rSurface
	g.text(x, y, fit(q.ID+" · "+typeName(q.Type), w), style{fg: rAccentInk, bg: b, bold: true})
	y++
	for _, l := range wrap(q.Prompt, w)[:min(2, len(wrap(q.Prompt, w)))] {
		g.text(x, y, l, style{fg: rInk, bg: b})
		y++
	}
	notes := questionNotes(q)
	if len(notes) == 0 {
		return y + 1
	}
	y++
	text := strings.Join(notes, " · ")
	lines := wrap(text, w-4)
	g.fill(x, y, w, len(lines)+1, rInverse)
	for i, l := range lines {
		g.text(x+2, y+i, l, style{fg: rOnInverse, bg: rInverse, bold: true})
	}
	return y + len(lines) + 2
}

// quadrant plots difficulty against discrimination. Questions worth a look
// carry their name; the rest are dots.
func (m *Report) quadrant(g *grid, x, y, w, h int, sel report.QuestionStat) {
	b := rSurface
	px, pw := x+1, w-2
	line := y + (h-1)*3/4
	for i := range pw {
		g.put(px+i, line, "·", style{fg: rInk3, bg: b})
	}
	g.vline(px+pw/2, y, h, rRule, b)
	g.text(px, y, T("report.ui.separates"), style{fg: rInk2, bg: b})
	g.text(px, y+h-1, T("report.ui.no_separation"), style{fg: rInk2, bg: b})
	g.text(px, y+h, T("report.ui.hard"), style{fg: rInk2, bg: b})
	g.right(px+pw, y+h, T("report.ui.easy"), style{fg: rInk2, bg: b})
	place := func(q report.QuestionStat) (int, int) {
		d := max(-0.2, min(0.5, *q.Discrimination))
		return px + int(q.Rate*float64(pw-5)), y + int((0.5-d)/0.7*float64(h-1))
	}
	for _, q := range m.r.Questions {
		if q.Discrimination == nil || q.ID == sel.ID {
			continue
		}
		cx, cy := place(q)
		if flagged(q) {
			g.text(cx, cy, q.ID, style{fg: rInk, bg: b, bold: true})
		} else {
			g.put(cx+1, cy, "●", style{fg: rInk2, bg: b})
		}
	}
	if sel.Discrimination != nil {
		cx, cy := place(sel)
		g.text(cx, cy, sel.ID, style{fg: rSurface, bg: rAccent, bold: true})
	}
}

func (m *Report) drawIncidents(g *grid, y int) int {
	b := rSurface
	inc := m.r.Incidents
	warn, high := 0, 0
	for _, k := range inc.ByKind {
		warn += k.Warn
		high += k.High
	}
	record(g, 0, y, g.w, [][2]string{{strconv.Itoa(warn), T("report.ui.warnings")}, {strconv.Itoa(high), T("report.ui.serious")},
		{strconv.Itoa(inc.Strikes), T("report.ui.active_strikes")}, {strconv.Itoa(inc.Reverted), T("report.ui.reverted")},
		{minutes(inc.TimeLostMin), T("report.ui.time_lost")}}, b)
	y += 4
	if warn+high == 0 {
		g.text(2, y, T("report.incidents.none"), style{fg: rInk2, bg: b})
		return y + 1
	}
	wide := g.w >= wideCols
	tw := g.w - 4
	if wide {
		tw = min(62, g.w/2)
	}
	section(g, 2, y, T("report.timeline.title"), T("report.timeline.step", "n", inc.Step), b)
	lx := g.text(2, y+1, "██", style{fg: rInk2, bg: b})
	lx = g.text(lx+1, y+1, T("report.timeline.warn"), style{fg: rInk2, bg: b})
	lx = g.text(lx+2, y+1, "██", style{fg: rInk, bg: b})
	g.text(lx+1, y+1, T("report.timeline.high"), style{fg: rInk2, bg: b})
	end := timeline(g, 2, y+3, tw, inc)
	if wide {
		kx := tw + 6
		g.vline(kx-2, y, 14, rRule, b)
		section(g, kx, y, T("report.col.kind"), "", b)
		end = max(end, kindBars(g, kx, y+2, g.w-kx-2, inc))
	} else {
		section(g, 2, end, T("report.col.kind"), "", b)
		end = kindBars(g, 2, end+2, g.w-4, inc)
	}
	y = end + 1
	section(g, 2, y, T("report.ui.who_title"), T("report.ui.who_hint"), b)
	y++
	label(g, 2, y, pad(T("monitor.col.student"), 28)+pad(T("report.col.warnings"), 10)+T("report.ui.signals"), b)
	y++
	g.hline(0, y, g.w, rRuleStrong, b)
	y++
	rows := slices.Clone(m.r.Students)
	slices.SortStableFunc(rows, func(a, c report.StudentLine) int { return c.Signals + c.Strikes - a.Signals - a.Strikes })
	for _, st := range rows[:min(8, len(rows))] {
		if st.Signals+st.Strikes == 0 {
			break
		}
		g.text(2, y, pad(st.Name, 28)+pad(tally(st.Strikes, max(3, st.Strikes)), 10)+strconv.Itoa(st.Signals), style{fg: rInk, bg: b})
		y++
	}
	return y
}

// timeline stacks warnings and serious signals per slot in half cells.
func timeline(g *grid, x, y, w int, inc report.Incidents) int {
	const rows = 6
	b := rSurface
	n := len(inc.Timeline)
	if n == 0 {
		return y
	}
	step := max(1, w/n)
	bw := max(1, step-1)
	top := 1
	for _, t := range inc.Timeline {
		top = max(top, t.Warn+t.High)
	}
	for i, t := range inc.Timeline {
		cx := x + i*step
		warnH := t.Warn * rows * 2 / top
		highH := t.High * rows * 2 / top
		if t.Warn > 0 {
			warnH = max(warnH, 1)
		}
		if t.High > 0 {
			highH = max(highH, 1)
		}
		half := func(k int) role {
			switch {
			case k < warnH:
				return rInk2
			case k < warnH+highH:
				return rInk
			}
			return b
		}
		for r := range rows {
			low, up := half(r*2), half(r*2+1)
			if low == b {
				continue
			}
			glyph, st := "█", style{fg: low, bg: b}
			if up != low {
				glyph, st = "▄", style{fg: low, bg: up}
			}
			for c := range bw {
				g.put(cx+c, y+rows-1-r, glyph, st)
			}
		}
	}
	g.hline(x, y+rows, min(w, n*step), rRuleStrong, b)
	every := max(1, 7/step+1)
	for i := 0; i < n; i += every {
		g.text(x+i*step, y+rows+1, strconv.Itoa(inc.Timeline[i].Minute)+"'", style{fg: rInk2, bg: b})
	}
	return y + rows + 3
}

// kindBars shows each kind of signal with its two inks and a number.
func kindBars(g *grid, x, y, w int, inc report.Incidents) int {
	b := rSurface
	top := 1
	for _, k := range inc.ByKind {
		top = max(top, k.Warn+k.High)
	}
	for _, k := range inc.ByKind {
		g.text(x, y, fit(signalName(k.Kind), w-4), style{fg: rInk, bg: b})
		g.right(x+w, y, strconv.Itoa(k.Warn+k.High), style{fg: rInk, bg: b, bold: true})
		warn, high := k.Warn*w/top, k.High*w/top
		for i := range warn {
			g.put(x+i, y+1, "▀", style{fg: rInk2, bg: b})
		}
		for i := range high {
			g.put(x+warn+i, y+1, "▀", style{fg: rInk, bg: b})
		}
		y += 2
	}
	return y + 1
}

func (m *Report) drawEvidence(g *grid, y int) int {
	b := rSurface
	wide := g.w >= wideCols
	lines := wrap(T("report.evidence.disclaimer"), g.w-4)
	g.fill(0, y-1, g.w, len(lines)+1, rSurface2)
	for i, l := range lines {
		g.text(2, y-1+i, l, style{fg: rInk, bg: rSurface2, bold: true})
	}
	y += len(lines) + 1
	section(g, 2, y, T("report.pairs.title"), fit(T("report.pairs.hint"), g.w-30), b)
	y += 2
	tw := g.w
	if wide {
		tw = min(72, g.w*3/5)
	}
	if len(m.r.Pairs) == 0 {
		g.text(2, y, T("report.pairs.none"), style{fg: rInk2, bg: b})
		y += 2
	} else {
		m.sel = min(max(0, m.sel), len(m.r.Pairs)-1)
		top := y
		bw := max(8, tw-34)
		scale := 1.0
		for _, p := range m.r.Pairs {
			scale = math.Max(scale, math.Max(float64(p.SameWrong), p.Expected))
		}
		for i, p := range m.r.Pairs {
			bg := b
			fg := rInk
			if i == m.sel {
				bg, fg = rSurface2, rAccentInk
				g.fill(0, y, tw, 4, bg)
				for j := range 4 {
					g.text(0, y+j, "▒", style{fg: rAccent, bg: bg})
				}
			}
			level := T("report.pairs.medium")
			levelStyle := style{fg: rInk, bg: bg, bold: true}
			if p.Level == report.LevelHigh {
				level = T("report.pairs.high")
				levelStyle = style{fg: rOnInverse, bg: rInverse, bold: true}
			}
			g.text(2, y, " "+pad(level, 6), levelStyle)
			g.text(10, y, fit(p.Students[0].Name+" ↔ "+p.Students[1].Name, tw-12), style{fg: fg, bg: bg, bold: true})
			g.text(10, y+1, pad(T("report.ui.same_errors"), 16), style{fg: rInk2, bg: bg})
			halfBar(g, 26, y+1, bw, float64(p.SameWrong)/scale, rInk, bg)
			g.text(27+bw, y+1, fmt.Sprintf("%d / %d", p.SameWrong, p.BothWrong), style{fg: rInk, bg: bg, bold: true})
			g.text(10, y+2, pad(T("report.ui.expected"), 16), style{fg: rInk2, bg: bg})
			halfBar(g, 26, y+2, bw, p.Expected/scale, rInk2, bg)
			g.text(27+bw, y+2, num(p.Expected), style{fg: rInk2, bg: bg})
			y += 5
		}
		p := m.r.Pairs[m.sel]
		x := 2
		dy := y
		w := g.w - 4
		if wide {
			x, dy, w = tw+3, top, g.w-tw-5
			g.vline(tw+1, top, max(y-top, 10), rRule, b)
		}
		section(g, x, dy, T("report.ui.why_title"), "", b)
		dy += 2
		for _, row := range [][2]string{{T("report.ui.chance"), odds(p.P)}, {T("report.ui.common"), strconv.Itoa(p.Common)},
			{T("report.ui.synced"), strconv.Itoa(p.Synced)}} {
			g.text(x, dy, pad(row[0], min(26, w/2)), style{fg: rInk2, bg: b})
			g.text(x+min(26, w/2), dy, fit(row[1], w/2), style{fg: rInk, bg: b, bold: true})
			dy++
		}
		if len(p.Questions) > 0 {
			dy++
			g.text(x, dy, fit(T("report.ui.same_questions"), w), style{fg: rInk2, bg: b})
			dy++
			cx := x
			for _, q := range p.Questions {
				if cx+ansi.StringWidth(q)+3 > x+w {
					dy++
					cx = x
				}
				cx = g.text(cx, dy, " "+q+" ", style{fg: rInk, bg: rSurface2, bold: true}) + 1
			}
			dy++
		}
		y = max(y, dy+1)
	}
	section(g, 2, y, T("monitor.away.title"), fit(T("report.away.hint"), g.w-30), b)
	y += 2
	if len(m.r.AwayHits) == 0 {
		g.text(2, y, T("report.away.none"), style{fg: rInk2, bg: b})
		return y + 1
	}
	for _, h := range m.r.AwayHits {
		hard := T("report.away.missed", "pct", pct(h.Difficulty*100))
		if h.Strong {
			hard = T("report.away.hard", "pct", pct(h.Difficulty*100))
		}
		line := pad(h.Name, 24) + pad(h.QuestionID, 7) + T("report.away.line", "signal", signalName(h.Signal), "away", seconds(h.AwayMs), "after", seconds(h.AfterMs), "difficulty", hard)
		g.text(2, y, fit(line, g.w-4), style{fg: rInk, bg: b, bold: h.Strong})
		y++
	}
	return y
}

func (m *Report) drawHistory(g *grid, y int) int {
	b := rSurface
	wide := g.w >= wideCols
	bw := 32
	if !wide {
		bw = 18
	}
	head := pad(T("report.history.sessions"), 22) + pad(T("report.history.code"), 9) + pad(T("report.history.submitted"), 10) + pad("", bw+1) + pad(T("report.history.mean"), 7) + T("report.history.median")
	label(g, 2, y, fit(head, g.w-3), b)
	g.hline(0, y+1, g.w, rRuleStrong, b)
	y += 2
	for _, h := range m.r.History {
		bg, fg, ink := b, rInk, rInk2
		if h.Current {
			bg, fg, ink = rSurface2, rAccentInk, rInk
			g.fill(0, y, g.w, 1, bg)
			g.text(0, y, "▒", style{fg: rAccent, bg: bg})
		}
		x := g.text(2, y, pad(day(h.StartedAt), 21)+" "+pad(h.Code, 8)+" "+pad(fmt.Sprintf("%d/%d", h.Submitted, h.Students), 9)+" ", style{fg: fg, bg: bg, bold: h.Current})
		halfBar(g, x, y, bw, h.Mean/100, ink, bg)
		g.put(x+min(bw-1, int(h.Median/100*float64(bw))), y, "│", style{fg: rInk, bg: bg, bold: true})
		x += bw + 1
		x = g.text(x, y, pad(pct(h.Mean), 7), style{fg: rInk, bg: bg, bold: true})
		x = g.text(x, y, pad(pct(h.Median), 7), style{fg: rInk2, bg: bg})
		if h.Current && wide {
			g.text(x, y, T("report.history.this"), style{fg: rInk2, bg: bg})
		}
		y += 2
	}
	y++
	section(g, 2, y, T("report.leaks.title"), fit(T("report.leaks.hint"), g.w-40), b)
	g.hline(0, y+1, g.w, rRuleStrong, b)
	y += 2
	switch {
	case len(m.r.History) < 2:
		g.text(2, y, T("report.leaks.need_more"), style{fg: rInk2, bg: b})
		return y + 1
	case len(m.r.Leaks) == 0:
		g.text(2, y, T("report.leaks.none"), style{fg: rInk2, bg: b})
		return y + 1
	}
	for _, l := range m.r.Leaks {
		g.text(2, y, pad(l.QuestionID, 7), style{fg: rInk, bg: b, bold: true})
		g.text(9, y, fit(l.Prompt, g.w-11), style{fg: rInk, bg: b})
		g.text(9, y+1, fit(T("report.leaks.line", "before", pct(l.Before*100), "before_n", l.BeforeN, "now", pct(l.Now*100), "now_n", l.NowN), g.w-11), style{fg: rInk2, bg: b})
		if wide {
			dumbbell(g, 74, y, g.w-78, l.Before, l.Now)
		}
		y += 3
	}
	return y
}

// dumbbell shows before ○ and now ● on one axis.
func dumbbell(g *grid, x, y, w int, before, now float64) {
	b := rSurface
	a, c := x+int(before*float64(w-1)), x+int(now*float64(w-1))
	for i := range w {
		g.put(x+i, y, "·", style{fg: rRule, bg: b})
	}
	for i := min(a, c); i <= max(a, c); i++ {
		g.put(i, y, "─", style{fg: rInk, bg: b})
	}
	g.put(a, y, "○", style{fg: rInk, bg: b})
	g.put(c, y, "●", style{fg: rInk, bg: b})
}
