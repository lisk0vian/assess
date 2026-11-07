package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/exam"
)

const editorBodyRows = 300 // long bodies draw here and show through a window

func (m *Editor) keyTable() []keyGroup {
	k := func(key, label string, bar bool) keyItem { return keyItem{keyHint{key, label}, bar} }
	quit := k("Esc", T("editor.ui.key_exit"), true)
	step := k("Tab", T("editor.ui.key_step"), true)
	switch {
	case m.q != nil:
		items := []keyItem{k("Tab", T("editor.ui.key_field"), true)}
		if m.q.field == qOptions {
			items = append(items, k("Space", T("editor.ui.key_mark"), true), k("Del", T("editor.key.delete"), false))
		}
		return []keyGroup{{T("wizard.step.questions"), append(items, k("Ctrl+E", T("editor.key.external"), true), k("Esc", T("editor.ui.key_done"), true))}}
	case m.pasting != nil:
		return []keyGroup{{T("editor.paste.title"), []keyItem{k("Enter", T("editor.paste.key_add"), true), k("Alt+Enter", T("editor.paste.key_line"), false), k("Esc", T("editor.paste.key_cancel"), true)}}}
	case m.step == stepDetails:
		return []keyGroup{{T("wizard.step.details"), []keyItem{k("↑↓", T("editor.ui.key_field"), true), k("←→", T("editor.ui.key_option"), true), step, quit}}}
	case m.step == stepRules:
		return []keyGroup{{T("wizard.step.proctoring"), []keyItem{k("↑↓", T("editor.ui.key_choose"), true), k("Space", T("editor.ui.key_activate"), true), k("←→", T("editor.ui.key_option"), false),
			step, k("?", T("monitor.ui.key_help"), true), quit}}}
	case m.step == stepReview:
		enter := T("editor.key.start")
		if m.d.Problems.HasErrors() {
			enter = T("editor.ui.key_go_problem")
		}
		return []keyGroup{{T("wizard.step.review"), []keyItem{k("↑↓", T("editor.ui.key_problem"), len(m.d.Problems) > 0), k("Enter", enter, true), k("Q", T("editor.ui.key_save_exit"), true),
			k("⇧Tab", T("wizard.key.back"), true), k("?", T("monitor.ui.key_help"), false)}}}
	}
	return []keyGroup{
		{T("wizard.step.questions"), []keyItem{k("↑↓", T("editor.ui.key_question"), true), k("Enter", T("editor.key.edit"), true), k("N", T("editor.key.new"), true), k("V", T("editor.paste.key_open"), true),
			k("D", T("editor.key.delete"), false), k("⇧↑↓", T("editor.key.move"), false), k("E", T("editor.key.external"), false), k("PgDn", T("editor.ui.key_read"), false)}},
		{T("editor.name"), []keyItem{step, k("?", T("monitor.ui.key_help"), true), k("C", T("editor.key.answers"), false), k("R", T("editor.ui.key_reload"), false),
			k("S", T("editor.key.start"), false), quit}},
	}
}

func (m *Editor) barKeys() []keyHint {
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

// View draws the exam screen on a grid the size of the window.
func (m *Editor) View() tea.View {
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

func (m *Editor) screen(g *grid) {
	y := m.shell(g)
	bottom := g.h - keyBar(g, m.barKeys())
	if m.notice != "" {
		bottom--
		st := style{fg: rInk, bg: rBg}
		if m.isErr {
			st = style{fg: rOnInverse, bg: rInverse, bold: true}
		}
		g.fill(0, bottom, g.w, 1, st.bg)
		g.text(1, bottom, fit(m.notice, g.w-2), st)
	}
	H := bottom - y
	g.fill(0, y, g.w, H, rBg)
	switch m.step {
	case stepDetails:
		m.drawDetails(g, y, H)
	case stepQuestions:
		m.drawQuestions(g, y, H)
	case stepRules:
		m.drawRules(g, y, H)
	case stepReview:
		m.drawReview(g, y, H)
	}
	if m.showHelp {
		helpPanel(g, m.keyTable())
	}
	if m.confirm != "" {
		confirmBox(g, m.confirm, "")
	}
}

func (m *Editor) shell(g *grid) int {
	b := rSurface
	g.fill(0, 0, g.w, 2, b)
	title, sub := m.title.value(), T("editor.ui.saves_itself")
	if m.d == nil || strings.TrimSpace(title) == "" {
		title = T("wizard.intro.title")
	}
	if m.d != nil {
		sub = T("wizard.n_questions", "n", len(m.d.Exam.Questions)) + " · " + humanDuration(m.durationValue())
	}
	end := g.w - 1
	if !m.savedAt.IsZero() {
		end = g.right(end, 0, "✓ "+T("editor.ui.saved", "time", m.savedAt.Format("15:04")), style{fg: rInk2, bg: b}) - ansi.StringWidth("✓ "+T("editor.ui.saved", "time", "00:00")) - 2
	}
	g.text(1, 0, fit(title, end-2), style{fg: rInk, bg: b, bold: true})
	g.text(1, 1, fit(sub, g.w-2), style{fg: rInk2, bg: b})
	g.hline(0, 2, g.w, rRuleStrong, rBg)
	names := make([]string, stepCount)
	for i, key := range stepKeys {
		names[i] = strconv.Itoa(i+1) + " " + strings.ToUpper(T(key))
	}
	tabBar(g, 3, names, m.step)
	return 4
}

func (m *Editor) durationValue() string {
	if m.durChoice < len(durationPresets) {
		return exam.FormatDuration(durationPresets[m.durChoice])
	}
	return m.typedDuration()
}

// body draws on a tall surface, shows the part around row focus, and returns
// the rows it used.
func body(g *grid, x, y, w, h, focus int, draw func(b *grid) int) int {
	b := newGrid(w, editorBodyRows, rSurface)
	used := min(draw(b), editorBodyRows)
	top := min(max(0, focus-h/2), max(0, used-h))
	for j := range h {
		if top+j < editorBodyRows && y+j < g.h {
			copy(g.c[(y+j)*g.w+x:(y+j)*g.w+x+w], b.c[(top+j)*w:(top+j+1)*w])
		}
	}
	return used
}

// chips draws a row of choices, wrapping at the edge of g, and returns the
// last row it used.
func chips(g *grid, x, y int, names []string, cur int, focus bool, bg role) int {
	start := x
	for i, n := range names {
		if x > start && x+ansi.StringWidth(n)+2 > g.w-1 {
			x, y = start, y+1
		}
		st := style{fg: rInk, bg: rSurface2}
		if i == cur {
			st = style{fg: rOnInverse, bg: rInverse, bold: true}
			if focus {
				g.put(x-1, y, "▒", style{fg: rAccent, bg: bg})
			}
		}
		x = g.text(x, y, " "+n+" ", st) + 1
	}
	return y
}

func (m *Editor) drawDetails(g *grid, y, H int) {
	big := g.w >= wideCols
	w, x := g.w-4, 2
	if big {
		w = 72
	}
	bg := rSurface
	sheet(g, x, y+1, w, H-3)
	const fx = 16
	durs := make([]string, 0, len(durationPresets)+1)
	for _, d := range durationPresets {
		durs = append(durs, T("editor.ui.minutes", "n", int(d.Minutes())))
	}
	durs = append(durs, T("editor.ui.other_duration"))
	langs := m.languages()
	langNames := make([]string, len(langs))
	for i, l := range langs {
		langNames[i] = languageName(l)
	}
	vis := []string{T("editor.ui.vis_none"), T("editor.ui.vis_score"), T("editor.ui.vis_review")}
	body(g, x, y+1, w, H-3, 1+3*m.field, func(b *grid) int {
		row := func(i int, name string) int {
			label(b, 2, 1+3*i, fit(name, fx-3), bg)
			return 1 + 3*i
		}
		fw := w - fx - 2
		yt := row(detTitle, strings.ToUpper(T("form.manifest.exam_title")))
		m.title.draw(b, fx, yt, fw, 1, m.field == detTitle, style{fg: rInk, bg: rSurface2, bold: true})
		b.text(fx+1, yt+1, fit(T("wizard.title_hint"), fw-1), style{fg: rInk2, bg: bg})
		yd := row(detDuration, strings.ToUpper(T("form.manifest.duration")))
		chips(b, fx, yd, durs, m.durChoice, m.field == detDuration, bg)
		if m.durChoice == len(durationPresets) {
			m.other.draw(b, fx, yd+1, 14, 1, m.field == detDuration, style{fg: rInk, bg: rSurface2, bold: true})
			b.text(fx+15, yd+1, T("wizard.duration_hint"), style{fg: rInk2, bg: bg})
		}
		chips(b, fx, row(detVisibility, T("editor.ui.at_end")), vis, slices.Index(visibilities, m.man.Visibility), m.field == detVisibility, bg)
		chips(b, fx, row(detLanguage, strings.ToUpper(T("wizard.review.language"))), langNames, slices.Index(langs, m.man.Language), m.field == detLanguage, bg)
		yi := row(detInstructions, T("editor.ui.instructions"))
		n := m.instr.draw(b, fx, yi, fw, 3, m.field == detInstructions, style{fg: rInk, bg: rSurface2})
		return yi + n + 1
	})
	if !big {
		return
	}
	X, c := x+w+3, g.w-x-w-5
	label(g, X, y+1, fit(T("editor.ui.student_view"), c), rBg)
	title := m.title.value()
	if title == "" {
		title = T("wizard.title_placeholder")
	}
	h := min(18, H-5)
	sheet(g, X, y+3, c-1, h)
	body(g, X, y+3, c-1, h, 0, func(b *grid) int {
		Y := para(b, 2, 1, c-5, title, style{fg: rInk, bg: bg, bold: true})
		Y = para(b, 2, Y, c-5, humanDuration(m.durationValue()), style{fg: rInk2, bg: bg})
		b.hline(0, Y, c-1, rRuleStrong, bg)
		Y = para(b, 2, Y+2, c-5, T("editor.ui.lobby."+m.man.Visibility), style{fg: rInk, bg: bg})
		b.text(2, Y+1, " "+T("editor.ui.in_lobby")+" ", style{fg: rOnInverse, bg: rInverse, bold: true})
		return Y + 3
	})
}

// ---------------- questions ----------------

func (m *Editor) drawRail(g *grid, y, h, w int) {
	rows := m.rows()
	top := min(max(0, m.sel-h/2), max(0, len(rows)-h))
	for i := range min(h, len(rows)-top) {
		r := rows[top+i]
		on, Y := top+i == m.sel, y+i
		bg := rBg
		if on {
			bg = rSurface2
		}
		g.fill(0, Y, w, 1, bg)
		if on {
			g.put(0, Y, "▒", style{fg: rAccent, bg: bg})
		}
		x := 2
		if r.inner {
			x = 3
		}
		bad := r.kind == rowQuestion && m.incomplete(r.id)
		switch {
		case r.kind == rowPool:
			g.text(x, Y, "▒▒", style{fg: rInk2, bg: bg})
		case bad:
			g.text(x, Y, "··", style{fg: rInk3, bg: bg})
		default:
			g.text(x, Y, "▀▀", style{fg: rInk, bg: bg})
		}
		x = g.text(x+3, Y, pad(r.id, 6), style{fg: rInk, bg: bg, bold: true})
		text := ""
		if q := m.d.Exam.Questions[r.id]; q != nil {
			text = plainPrompt(q.Prompt)
		} else if r.kind == rowPool {
			p := m.d.Exam.Manifest.Items[r.item].Pool
			text = T("editor.ui.pool", "pick", p.Pick, "of", len(p.From))
		}
		end := w - 1
		if bad {
			end -= 2
			g.text(w-2, Y, "!", style{fg: rInk, bg: bg, bold: true})
		}
		g.text(x, Y, fit(text, end-x), style{fg: rInk2, bg: bg})
	}
	g.vline(w, y, h, rRuleStrong, rBg)
}

func (m *Editor) drawQuestions(g *grid, y, H int) {
	big := g.w >= wideCols
	rw := 22
	if big {
		rw = 34
	}
	rows := m.rows()
	m.selected()
	x, sw := rw+1, g.w-rw-2
	if big {
		sw = min(sw, 56)
	}
	if m.pasting != nil {
		m.drawPaste(g, y, H)
		return
	}
	if len(rows) == 0 && m.q == nil {
		sheet(g, 2, y+1, g.w-5, max(3, H-3))
		g.text(4, y+2, T("editor.ui.no_questions"), style{fg: rInk, bg: rSurface, bold: true})
		g.text(4, y+3, fit(T("editor.ui.no_questions_hint"), g.w-9), style{fg: rInk2, bg: rSurface})
		return
	}
	m.drawRail(g, y, H, rw)
	sheet(g, x, y+1, sw-1, H-3)
	if m.q != nil {
		m.drawWriting(g, x, y, sw, H, big)
		return
	}
	row, _ := m.selected()
	if q := m.d.Exam.Questions[row.id]; q != nil {
		a := m.d.Exam.Key.Answers[row.id]
		used := body(g, x, y+1, sw-1, H-3, m.scroll+(H-3)/2, func(b *grid) int {
			limit := "—"
			if q.TimeLimit != 0 {
				limit = exam.FormatDuration(q.TimeLimit.D())
			}
			record(b, 0, 0, sw-1, [][2]string{{q.ID, T("editor.ui.question")}, {typeName(q.Type), T("editor.ui.type")},
				{fmtNum(a.Points), T("editor.ui.points")}, {limit, T("editor.ui.time")}}, rSurface)
			return drawQuestion(b, 2, 4, sw-5, q, a, true) + 1
		})
		m.scroll = min(m.scroll, max(0, used-(H-3)))
	} else if row.kind == rowPool {
		p := m.d.Exam.Manifest.Items[row.item].Pool
		g.text(x+2, y+2, fit(T("editor.ui.pool", "pick", p.Pick, "of", len(p.From)), sw-5), style{fg: rInk, bg: rSurface, bold: true})
		para(g, x+2, y+4, sw-5, T("editor.ui.pool_hint"), style{fg: rInk2, bg: rSurface})
	}
	if big {
		m.drawTotals(g, x+sw+2, y+1, g.w-x-sw-3)
	}
}

func (m *Editor) drawTotals(g *grid, X, y, c int) {
	bg := rBg
	if c < 18 {
		return
	}
	bad := 0
	types := map[string]int{}
	for id, q := range m.d.Exam.Questions {
		types[q.Type]++
		if m.incomplete(id) {
			bad++
		}
	}
	n := len(m.d.Exam.Questions)
	label(g, X, y, T("editor.ui.status"), bg)
	for i, r := range [][2]string{{strconv.Itoa(n), T("editor.ui.total")}, {strconv.Itoa(n - bad), T("editor.ui.ready")}, {strconv.Itoa(bad), T("editor.ui.to_fix")}} {
		g.text(X, y+2+i, fmt.Sprintf("%3s", r[0]), style{fg: rInk, bg: bg, bold: true})
		g.text(X+4, y+2+i, fit(r[1], c-4), style{fg: rInk2, bg: bg})
	}
	label(g, X, y+6, T("editor.ui.by_type"), bg)
	most := 1
	for _, t := range questionTypes {
		most = max(most, types[t])
	}
	for i, t := range questionTypes {
		Y := y + 7 + i
		g.text(X, Y, pad(T("editor.ui.types."+t), 9), style{fg: rInk2, bg: bg})
		halfBar(g, X+9, Y, c-13, float64(types[t])/float64(most), rInk, bg)
		g.right(X+c, Y, strconv.Itoa(types[t]), style{fg: rInk, bg: bg, bold: true})
	}
	label(g, X, y+12, strings.ToUpper(T("editor.ui.points")), bg)
	g.text(X, y+13, fit(T("editor.ui.points_total", "n", fmtNum(m.totalPoints())), c), style{fg: rInk, bg: bg})
}

// totalPoints is what one student can score: a pool counts the questions it picks.
func (m *Editor) totalPoints() float64 {
	total := 0.0
	key := m.d.Exam.Key.Answers
	for _, it := range m.d.Exam.Manifest.Items {
		if it.Pool != nil && len(it.Pool.From) > 0 {
			total += key[it.Pool.From[0]].Points * float64(it.Pool.Pick)
			continue
		}
		total += key[it.Question].Points
	}
	return total
}

// drawQuestion draws a question from y and returns the row after it. The
// teacher sees the right answer in magenta; the student view does not.
func drawQuestion(g *grid, x, y, w int, q *exam.Question, a exam.Answer, teacher bool) int {
	bg := rSurface
	code := false
	for l := range strings.SplitSeq(strings.TrimSpace(q.Prompt), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			code = !code
			continue
		}
		if code {
			g.fill(x, y, w, 1, rSurface2)
			g.text(x+1, y, fit(l, w-2), style{fg: rInk, bg: rSurface2})
			y++
			continue
		}
		if strings.TrimSpace(l) == "" {
			y++
			continue
		}
		for _, r := range wrap(l, w) {
			g.text(x, y, r, style{fg: rInk, bg: bg, bold: true})
			y++
		}
	}
	y++
	option := func(letter, text string, right bool) {
		mark := style{fg: rInk, bg: rSurface2, bold: true}
		if right {
			mark = style{fg: rSurface, bg: rAccent, bold: true}
		}
		g.text(x, y, " "+letter+" ", mark)
		tw := w - 4
		if right {
			tw -= 10
			g.right(x+w, y, T("editor.ui.correct"), style{fg: rAccentInk, bg: bg, bold: true})
		}
		for _, r := range wrap(text, tw) {
			g.text(x+4, y, r, style{fg: rInk, bg: bg, bold: right})
			y++
		}
	}
	note := func(s string, st style) {
		for _, r := range wrap(s, w) {
			g.text(x, y, r, st)
			y++
		}
	}
	soft, mine := style{fg: rInk2, bg: bg}, style{fg: rAccentInk, bg: bg, bold: true}
	switch q.Type {
	case exam.TypeSingle, exam.TypeMultiple:
		for i, o := range q.Options {
			right := o.ID == a.Option || slices.Contains(a.Options, o.ID)
			option(string(rune('A'+i)), o.Text, teacher && right)
		}
		if q.Type == exam.TypeMultiple && teacher {
			y++
			if a.Partial {
				note(T("wizard.preview.partial"), soft)
			} else {
				note(T("wizard.preview.all_or_nothing"), soft)
			}
		}
	case exam.TypeTrueFalse:
		option("V", T("wizard.preview.true"), teacher && a.Value != nil && *a.Value)
		option("F", T("wizard.preview.false"), teacher && a.Value != nil && !*a.Value)
	case exam.TypeShort:
		if !teacher {
			hint := T("wizard.preview.write_text")
			if q.Input == "number" {
				hint = T("wizard.preview.write_number")
			}
			rows := wrap(hint, min(w, 32)-2)
			g.fill(x, y, min(w, 32), len(rows), rSurface2)
			y = para(g, x+1, y, min(w, 32)-2, hint, style{fg: rInk2, bg: rSurface2})
			break
		}
		switch {
		case q.Input == "number" && a.Number != nil && a.Tolerance != nil && *a.Tolerance > 0:
			note(T("editor.ui.accept_margin", "v", num(*a.Number), "t", num(*a.Tolerance)), mine)
		case q.Input == "number" && a.Number != nil:
			note(T("editor.ui.accept_exact", "v", num(*a.Number)), mine)
		case len(a.Accept) > 0:
			note(T("wizard.preview.accepted", "list", strings.Join(a.Accept, " · ")), mine)
			note(T("wizard.preview.lenient"), soft)
		}
	}
	return y
}

func (m *Editor) drawWriting(g *grid, x, y, sw, H int, big bool) {
	e, bg := m.q, rSurface
	focusRow := map[int]int{}
	const fx = 15
	draw := func(b *grid) int {
		w := sw - 1
		Y := 1
		row := func(f int, name string) int {
			focusRow[f] = Y
			label(b, 2, Y, fit(name, fx-3), bg)
			return Y
		}
		line := func(f int, name string, t *textField) {
			t.draw(b, fx, row(f, name), min(24, w-fx-2), 1, e.field == f, style{fg: rInk, bg: rSurface2, bold: true})
			Y += 2
		}
		pick := func(f int, name string, names []string, cur int) {
			Y = chips(b, fx, row(f, name), names, cur, e.field == f, bg) + 2
		}
		names := make([]string, len(questionTypes))
		for i, t := range questionTypes {
			names[i] = T("editor.ui.types." + t)
		}
		pick(qType, T("editor.ui.type_label"), names, slices.Index(questionTypes, e.v.Type))
		Y += e.prompt.draw(b, fx, row(qPrompt, T("editor.ui.prompt")), w-fx-2, 4, e.field == qPrompt, style{fg: rInk, bg: rSurface2, bold: true}) + 1
		soft := style{fg: rInk2, bg: bg}
		switch e.v.Type {
		case exam.TypeSingle, exam.TypeMultiple:
			row(qOptions, T("editor.ui.options"))
			Y = para(b, fx, Y, w-fx-2, T("editor.ui.mark_hint"), soft)
			for i := range e.opts {
				on := e.field == qOptions && i == e.opt
				focusRow[qOptions+100+i] = Y
				mark := style{fg: rInk, bg: rSurface2, bold: true}
				if e.marks[i] {
					mark = style{fg: rSurface, bg: rAccent, bold: true}
				}
				if on {
					b.put(1, Y, "▒", style{fg: rAccent, bg: bg})
				}
				b.text(2, Y, " "+string(rune('A'+i))+" ", mark)
				fb := bg
				if on {
					fb = rSurface2
				}
				e.opts[i].draw(b, 6, Y, w-8-10, 1, on && e.typing, style{fg: rInk, bg: fb, bold: e.marks[i]})
				if e.marks[i] {
					b.right(w-2, Y, T("editor.ui.correct"), style{fg: rAccentInk, bg: bg, bold: true})
				}
				Y++
			}
			Y++
			if e.v.Type == exam.TypeMultiple {
				cur := 0
				if e.v.Partial {
					cur = 1
				}
				pick(qPartial, T("editor.ui.grading"), []string{T("editor.ui.all_or_nothing"), T("editor.ui.partial")}, cur)
			}
		case exam.TypeTrueFalse:
			cur := -1
			if e.v.Value != nil {
				cur = 1
				if *e.v.Value {
					cur = 0
				}
			}
			pick(qValue, T("editor.ui.right_answer"), []string{T("wizard.preview.true"), T("wizard.preview.false")}, cur)
		case exam.TypeShort:
			cur := 0
			if e.v.Input == "text" {
				cur = 1
			}
			pick(qInput, T("editor.ui.answer_is"), []string{T("form.input.number"), T("form.input.text")}, cur)
			if e.v.Input == "number" {
				line(qNumber, T("editor.ui.right_answer"), &e.number)
				line(qTolerance, T("editor.ui.margin"), &e.tolerance)
			} else {
				Y += e.accept.draw(b, fx, row(qAccept, T("editor.ui.accepted")), w-fx-2, 3, e.field == qAccept, style{fg: rInk, bg: rSurface2})
				Y = para(b, fx, Y, w-fx-2, T("wizard.preview.lenient"), soft) + 1
			}
		}
		line(qPoints, strings.ToUpper(T("editor.ui.points")), &e.points)
		line(qLimit, T("editor.ui.limit"), &e.limit)
		if e.v.Type == exam.TypeSingle || e.v.Type == exam.TypeMultiple {
			pick(qReveal, T("editor.ui.reveal"), []string{T("form.inherit"), T("form.reveal.always"), T("form.reveal.hover")}, slices.Index(revealModes, e.v.Reveal))
		}
		pick(qLens, T("editor.ui.lens"), []string{T("form.inherit"), T("common.yes"), T("common.no")}, slices.Index(lensModes, e.v.Lens))
		return Y
	}
	// A first pass finds the focused row; the window follows it.
	draw(newGrid(sw-1, editorBodyRows, rSurface))
	focus := focusRow[e.field]
	if e.field == qOptions {
		focus = focusRow[qOptions+100+e.opt]
	}
	body(g, x, y+1, sw-1, H-3, focus, draw)
	if !big {
		return
	}
	P, c := x+sw+2, g.w-x-sw-3
	if c < 20 {
		return
	}
	label(g, P, y+1, fit(T("editor.ui.student_view"), c), rBg)
	sheet(g, P, y+3, c-1, min(16, H-5))
	e.sync()
	if q, a, err := e.v.build(); err == nil {
		body(g, P, y+3, c-1, min(16, H-5), 0, func(b *grid) int {
			b.text(2, 1, q.ID, style{fg: rInk2, bg: bg})
			return drawQuestion(b, 2, 2, c-5, q, a, false)
		})
	}
}

// ---------------- rules and review ----------------

func (m *Editor) drawRules(g *grid, y, H int) {
	big := g.w >= wideCols
	w, x := g.w-4, 2
	if big {
		w = 56
	}
	bg := rSurface
	sheet(g, x, y+1, w, H-3)
	cur := max(0, slices.Index(profiles, m.man.Profile))
	focus := 0
	draw := func(b *grid) int {
		label(b, 2, 1, T("editor.ui.how_proctored"), bg)
		for i, name := range []string{T("wizard.profile.standard"), T("wizard.profile.strict"), T("wizard.profile.off")} {
			Y := 3 + i*2
			rowBg := bg
			if m.field == i {
				rowBg, focus = rSurface2, Y
			}
			b.fill(0, Y, w, 1, rowBg)
			if m.field == i {
				b.put(0, Y, "▒", style{fg: rAccent, bg: rowBg})
			}
			mark, st := " ○ ", style{fg: rInk, bg: rSurface2, bold: true}
			if i == cur {
				mark, st = " ● ", style{fg: rSurface, bg: rAccent, bold: true}
			}
			b.text(2, Y, mark, st)
			b.text(6, Y, fit(name, w-8), style{fg: rInk, bg: rowBg, bold: i == cur})
			b.hline(0, Y+1, w, rRule, bg)
		}
		label(b, 2, 10, T("editor.ui.options"), bg)
		Y := 12
		toggle := func(f int, name string, on bool) {
			if m.field == f {
				focus = Y
				b.put(0, Y, "▒", style{fg: rAccent, bg: bg})
			}
			mark := "□"
			if on {
				mark = "■"
			}
			b.text(2, Y, mark, style{fg: rInk, bg: bg, bold: true})
			Y = para(b, 4, Y, w-6, name, style{fg: rInk, bg: bg})
		}
		value := func(f int, name, val string) {
			if m.field == f {
				focus = Y
				b.put(0, Y, "▒", style{fg: rAccent, bg: bg})
			}
			Y = para(b, 4, Y, w-6, name, style{fg: rInk, bg: bg})
			b.text(4, Y, "‹ "+val+" ›", style{fg: rInk, bg: bg, bold: true})
			Y++
		}
		toggle(ruleShuffleQ, T("form.manifest.shuffle_q"), m.man.ShuffleQ)
		toggle(ruleShuffleO, T("form.manifest.shuffle_o"), m.man.ShuffleO)
		if m.man.Profile != "off" {
			strikes := m.man.StrikeLimit
			if strikes == "" {
				strikes = T("editor.ui.by_profile")
			}
			value(ruleStrikes, T("form.manifest.strikes"), strikes)
			toggle(ruleLens, T("form.manifest.lens"), m.man.Lens)
			toggle(ruleAutoRead, T("form.manifest.auto_read"), m.man.AutoRead)
			value(ruleReadSpeed, T("form.manifest.read_speed"), T("form.read_speed."+m.man.ReadSpeed))
			toggle(ruleFrozen, T("wizard.frozen"), m.man.FrozenPointer)
		}
		if !big {
			Y++
			Y = para(b, 2, Y, w-4, T("wizard.profile_text."+m.man.Profile), style{fg: rInk2, bg: bg})
		}
		return Y + 1
	}
	draw(newGrid(w, editorBodyRows, rSurface)) // finds the focused row; the window follows it
	body(g, x, y+1, w, H-3, focus, draw)
	if !big {
		return
	}
	X, c := x+w+4, g.w-x-w-6
	label(g, X, y+2, fit(T("wizard.students_notice"), c), rBg)
	Y := y + 4
	for _, l := range wrap(T("wizard.profile_text."+m.man.Profile), c) {
		g.text(X, Y, l, style{fg: rInk, bg: rBg})
		Y++
	}
	Y++
	label(g, X, Y, fit(T("editor.ui.you_see"), c), rBg)
	Y += 2
	for _, key := range []string{"you_see_1", "you_see_2", "you_see_3"} {
		Y = para(g, X, Y, c, T("editor.ui."+key), style{fg: rInk2, bg: rBg})
	}
}

func (m *Editor) drawReview(g *grid, y, H int) {
	bg := rSurface
	ps := m.d.Problems
	m.sel = min(m.sel, max(0, len(ps)-1))
	x := 2
	facts := func(b *grid, w int) {
		record(b, 0, 0, w, [][2]string{{strconv.Itoa(len(m.d.Exam.Questions)), T("editor.ui.total")}, {fmtNum(m.totalPoints()), T("editor.ui.points")},
			{humanDuration(m.durationValue()), T("form.manifest.duration")}, {T("profile." + m.man.Profile), T("form.manifest.proctoring")}}, bg)
	}
	if g.w < wideCols {
		w := g.w - 4
		sheet(g, x, y+1, w, H-3)
		_, focus := m.drawTodo(newGrid(w, editorBodyRows, bg), 6, w)
		body(g, x, y+1, w, H-3, focus, func(b *grid) int {
			facts(b, w)
			label(b, 2, 4, T("editor.ui.before_start"), bg)
			end, _ := m.drawTodo(b, 6, w)
			return end
		})
		return
	}
	w := 58
	sheet(g, x, y+1, w, H-3)
	body(g, x, y+1, w, 4, 0, func(b *grid) int {
		facts(b, w)
		return 4
	})
	bx, bw := x+2, w-4
	if ps.HasErrors() {
		g.fill(bx, y+6, bw, 4, rSurface2)
		g.text(bx+2, y+7, fit(T("editor.ui.start_block"), bw-4), style{fg: rInk2, bg: rSurface2, bold: true})
		g.text(bx+2, y+8, fit(T("editor.ui.fix_n", "n", ps.Count(exam.SeverityError)), bw-4), style{fg: rInk2, bg: rSurface2})
	} else {
		g.fill(bx, y+6, bw, 4, rInverse)
		g.text(bx+2, y+7, fit(T("editor.ui.start_block"), bw-4), style{fg: rOnInverse, bg: rInverse, bold: true})
		g.text(bx+2, y+8, "Enter", style{fg: rOnInverse2, bg: rInverse})
	}
	g.text(bx+2, y+11, fit(T("editor.ui.save_exit_block"), bw-4), style{fg: rInk, bg: bg, bold: true})
	g.text(bx+2, y+12, fit(T("editor.ui.save_exit_hint"), bw-4), style{fg: rInk2, bg: bg})

	X, c := x+w+4, g.w-x-w-7
	label(g, X, y+1, fit(T("editor.ui.before_start"), c), rBg)
	if n := ps.Count(exam.SeverityError); n > 0 {
		g.right(X+c, y+1, T("editor.ui.to_fix_n", "n", n), style{fg: rInk, bg: rBg, bold: true})
	}
	sheet(g, X, y+3, c, H-5)
	_, focus := m.drawTodo(newGrid(c, editorBodyRows, bg), 1, c)
	body(g, X, y+3, c, H-5, focus, func(b *grid) int {
		end, _ := m.drawTodo(b, 1, c)
		return end
	})
}

// todoText says what a problem asks of the teacher, and where. The validator
// speaks in file fields; a missing answer is the one problem every new exam hits.
func (m *Editor) todoText(p exam.Problem) (where, what string) {
	where = p.File
	if id := m.problemQuestion(p); id != "" {
		where = id
		if q := m.d.Exam.Questions[id]; q != nil && plainPrompt(q.Prompt) != "" {
			where += "  " + plainPrompt(q.Prompt)
		}
	} else if p.File == exam.ManifestFile {
		where = T("wizard.step.details")
	}
	if p.File == exam.AnswerKeyFile && strings.HasPrefix(p.Message, "required:") {
		return where, T("editor.ui.todo_answer")
	}
	if p.Field != "" {
		return where, p.Field + ": " + p.Message
	}
	return where, p.Message
}

// drawTodo lists what is left to do, each item whole, and returns the row
// after the list and the row of the selected item.
func (m *Editor) drawTodo(b *grid, y, w int) (end, focus int) {
	bg := rSurface
	ps := m.d.Problems
	if len(ps) == 0 {
		b.text(2, y, fit("✓ "+T("wizard.review.ok"), w-4), style{fg: rInk, bg: bg, bold: true})
		return y + 2, y
	}
	focus = y
	for i, p := range ps {
		where, what := m.todoText(p)
		rows := wrap(what, w-7)
		on := i == m.sel
		rowBg, mark := bg, "·"
		if p.Severity == exam.SeverityError {
			mark = "□"
		}
		if on {
			rowBg = rSurface2
			focus = y
			b.fill(0, y, w, 1+len(rows), rowBg)
			for j := range 1 + len(rows) {
				b.put(0, y+j, "▒", style{fg: rAccent, bg: rowBg})
			}
		}
		b.text(2, y, mark, style{fg: rInk, bg: rowBg, bold: true})
		b.text(4, y, fit(where, w-6), style{fg: rInk, bg: rowBg, bold: true})
		for j, l := range rows {
			b.text(4, y+1+j, l, style{fg: rInk2, bg: rowBg})
		}
		y += 2 + len(rows)
	}
	return y, focus
}
