package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/session"
)

// Fixer corrects a finished session's questions from the report. Every
// change is previewed first, recorded, and can be undone.
type Fixer struct {
	Question    func(id string) *exam.Question
	Corrections func() []session.Correction
	Preview     func(c session.Correction) ([]session.ScoreChange, error)
	Apply       func(c session.Correction) (*report.Report, error)
	Undo        func(id int64) (*report.Report, error)
}

type fixState struct {
	q       *exam.Question
	void    bool // the kind: void for everyone, or accept another answer
	field   int  // 0 the kind, 1 the answer
	choice  int
	marks   []bool // multiple choice: the set to accept
	text    textField
	changes []session.ScoreChange
	problem string
}

func (m *Report) corrections(q string) []session.Correction {
	if m.Fix == nil {
		return nil
	}
	if m.corr == nil {
		m.corr = m.Fix.Corrections()
	}
	var out []session.Correction
	for _, c := range m.corr {
		if c.QuestionID == q {
			out = append(out, c)
		}
	}
	return out
}

func (m *Report) openFix(id string) {
	q := m.Fix.Question(id)
	if q == nil {
		return
	}
	m.fixing = &fixState{q: q, marks: make([]bool, len(q.Options))}
	m.preview()
}

// choices are the answers that can be accepted, when the type has a list.
func (f *fixState) choices() []string {
	switch f.q.Type {
	case exam.TypeSingle, exam.TypeMultiple:
		out := make([]string, len(f.q.Options))
		for i, o := range f.q.Options {
			out[i] = string(rune('A'+i)) + " " + o.Text
		}
		return out
	case exam.TypeTrueFalse:
		return []string{T("wizard.preview.true"), T("wizard.preview.false")}
	}
	return nil
}

func (f *fixState) correction() session.Correction {
	c := session.Correction{QuestionID: f.q.ID, Kind: session.CorrectAccept}
	switch {
	case f.void:
		c.Kind = session.CorrectVoid
	case f.q.Type == exam.TypeSingle:
		c.Value = f.q.Options[f.choice].ID
	case f.q.Type == exam.TypeMultiple:
		var ids []string
		for i, on := range f.marks {
			if on {
				ids = append(ids, f.q.Options[i].ID)
			}
		}
		c.Value = strings.Join(ids, ",")
	case f.q.Type == exam.TypeTrueFalse:
		c.Value = strconv.FormatBool(f.choice == 0)
	default:
		c.Value = f.text.value()
	}
	return c
}

func (m *Report) preview() {
	f := m.fixing
	f.changes, f.problem = nil, ""
	c := f.correction()
	if !f.void && strings.TrimSpace(c.Value) == "" {
		return
	}
	ch, err := m.Fix.Preview(c)
	if err != nil {
		f.problem = err.Error()
		return
	}
	f.changes = ch
}

func (m *Report) fixKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.fixing
	k := msg.String()
	list := f.choices()
	switch {
	case k == "esc" || k == "ctrl+c":
		m.fixing = nil
		return nil
	case k == "up":
		f.field = 0
	case k == "down" && !f.void:
		f.field = 1
	case k == "enter":
		c := f.correction()
		if f.problem != "" || (!f.void && strings.TrimSpace(c.Value) == "") {
			return nil
		}
		r, err := m.Fix.Apply(c)
		if err != nil {
			f.problem = err.Error()
			return nil
		}
		m.r, m.corr, m.fixing = r, nil, nil
		m.notice = T("report.fix.done", "n", len(f.changes))
		return nil
	case k == "u" && f.field == 0 || k == "U":
		if done := m.corrections(f.q.ID); len(done) > 0 {
			r, err := m.Fix.Undo(done[len(done)-1].ID)
			if err != nil {
				f.problem = err.Error()
				return nil
			}
			m.r, m.corr = r, nil
			m.notice = T("report.fix.undone")
		}
	case f.field == 0 && (k == "left" || k == "right"):
		f.void = !f.void
	case f.field == 1 && list != nil && (k == "left" || k == "right"):
		d := 1
		if k == "left" {
			d = -1
		}
		f.choice = (f.choice + d + len(list)) % len(list)
	case f.field == 1 && f.q.Type == exam.TypeMultiple && (k == "space" || k == " "):
		f.marks[f.choice] = !f.marks[f.choice]
	case f.field == 1 && list == nil:
		f.text.key(msg, false)
	default:
		return nil
	}
	m.preview()
	return nil
}

func correctionText(c session.Correction, q *exam.Question) string {
	if c.Kind == session.CorrectVoid {
		return T("report.fix.voided")
	}
	value := c.Value
	if q != nil {
		for i, o := range q.Options {
			value = strings.ReplaceAll(value, o.ID, string(rune('A'+i)))
		}
	}
	return T("report.fix.accepted", "value", value)
}

func (m *Report) fixKeys() []keyHint {
	keys := []keyHint{{"↑↓", T("editor.ui.key_field")}, {"←→", T("editor.ui.key_option")}}
	if m.fixing.q.Type == exam.TypeMultiple {
		keys = append(keys, keyHint{"Space", T("editor.ui.key_mark")})
	}
	keys = append(keys, keyHint{"Enter", T("report.fix.key_apply")})
	if len(m.corrections(m.fixing.q.ID)) > 0 {
		keys = append(keys, keyHint{"U", T("report.fix.key_undo")})
	}
	return append(keys, keyHint{"Esc", T("hub.key.back")})
}

func (m *Report) drawFix(g *grid) {
	f := m.fixing
	w := min(80, g.w-4)
	h := g.h - 6
	x, y := (g.w-w)/2, 2
	panel(g, x, y, w, h, fit(T("report.fix.title", "q", f.q.ID), w-4))
	b := rSurface
	body(g, x+1, y+2, w-2, h-3, 0, func(s *grid) int {
		cw := w - 6
		Y := para(s, 2, 0, cw, plainPrompt(f.q.Prompt), style{fg: rInk2, bg: b}) + 1
		for _, c := range m.corrections(f.q.ID) {
			Y = para(s, 2, Y, cw, "· "+correctionText(c, f.q)+" · "+c.At.Local().Format("15:04"), style{fg: rInk, bg: b, bold: true})
		}
		kinds := []string{T("report.fix.accept"), T("report.fix.void")}
		cur := 0
		if f.void {
			cur = 1
		}
		Y = chips(s, 2, Y+1, kinds, cur, f.field == 0, b) + 2
		if !f.void {
			label(s, 2, Y, T("report.fix.also"), b)
			Y++
			if list := f.choices(); list != nil {
				for i, l := range list {
					st := style{fg: rInk, bg: b}
					mark := "  "
					if f.q.Type == exam.TypeMultiple && f.marks[i] {
						mark = "■ "
					}
					if i == f.choice {
						st = style{fg: rAccentInk, bg: rSurface2, bold: true}
						s.fill(2, Y, cw, 1, rSurface2)
						if f.field == 1 {
							s.put(1, Y, "▒", style{fg: rAccent, bg: b})
						}
					}
					s.text(3, Y, fit(mark+l, cw-2), st)
					Y++
				}
			} else {
				f.text.draw(s, 2, Y, min(40, cw), 1, f.field == 1, style{fg: rInk, bg: rSurface2, bold: true})
				Y++
			}
			Y++
		} else {
			Y = para(s, 2, Y, cw, T("report.fix.void_hint"), style{fg: rInk2, bg: b}) + 1
		}
		if f.problem != "" {
			rows := wrap(f.problem, cw-2)
			s.fill(1, Y, cw+2, len(rows), rInverse)
			for i, l := range rows {
				s.text(2, Y+i, l, style{fg: rOnInverse, bg: rInverse, bold: true})
			}
			return Y + len(rows) + 1
		}
		label(s, 2, Y, T("report.fix.who", "n", len(f.changes)), b)
		Y += 2
		if len(f.changes) == 0 {
			Y = para(s, 2, Y, cw, T("report.fix.nobody"), style{fg: rInk2, bg: b})
		}
		for _, c := range f.changes {
			s.text(2, Y, fit(c.Name, cw-18), style{fg: rInk, bg: b})
			s.right(2+cw, Y, percent(c.Before)+" → "+percent(c.After), style{fg: rInk, bg: b, bold: true})
			Y++
		}
		return para(s, 2, Y+1, cw, T("report.fix.note"), style{fg: rInk2, bg: b})
	})
}

// correctedNote marks a question that was corrected, for its detail.
func (m *Report) correctedNote(q report.QuestionStat) string {
	var parts []string
	var eq *exam.Question
	if m.Fix != nil {
		eq = m.Fix.Question(q.ID)
	}
	for _, c := range m.corrections(q.ID) {
		parts = append(parts, correctionText(c, eq))
	}
	return strings.Join(parts, " · ")
}
