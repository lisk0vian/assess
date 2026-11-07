package tui

import (
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/exam"
)

// Fields of the writing form.
const (
	qType = iota
	qPrompt
	qOptions
	qPartial
	qValue
	qInput
	qNumber
	qTolerance
	qAccept
	qPoints
	qLimit
	qReveal
	qLens
)

var (
	questionTypes = []string{exam.TypeSingle, exam.TypeMultiple, exam.TypeTrueFalse, exam.TypeShort}
	revealModes   = []string{"", "always", "hover"}
	lensModes     = []string{"", "on", "off"}
)

// questionEdit is one question being written. Empty and half-written values
// are welcome: Review is what asks for them.
type questionEdit struct {
	v     *questionValues
	isNew bool
	field int

	prompt, points, limit, number, tolerance, accept textField

	opts   []textField // the last one is the empty row that adds an option
	marks  []bool
	opt    int
	typing bool // writing the option's text, so Space is a space
}

func newQuestionEdit(v *questionValues, isNew bool) *questionEdit {
	e := &questionEdit{v: v, isNew: isNew, field: qPrompt, prompt: newField(v.Prompt), points: newField(v.Points), limit: newField(v.TimeLimit),
		number: newField(v.Number), tolerance: newField(v.Tolerance), accept: newField(v.Accept)}
	for i, t := range lines(v.Options) {
		e.opts = append(e.opts, newField(t))
		e.marks = append(e.marks, i == v.Correct || slices.Contains(v.CorrectM, i))
	}
	e.addRow()
	return e
}

func (e *questionEdit) addRow() {
	if n := len(e.opts); n < exam.MaxOptions && (n == 0 || e.opts[n-1].value() != "") {
		e.opts = append(e.opts, textField{})
		e.marks = append(e.marks, false)
	}
}

func (e *questionEdit) fields() []int {
	out := []int{qType, qPrompt}
	switch e.v.Type {
	case exam.TypeSingle:
		out = append(out, qOptions)
	case exam.TypeMultiple:
		out = append(out, qOptions, qPartial)
	case exam.TypeTrueFalse:
		out = append(out, qValue)
	case exam.TypeShort:
		out = append(out, qInput)
		if e.v.Input == "number" {
			out = append(out, qNumber, qTolerance)
		} else {
			out = append(out, qAccept)
		}
	}
	out = append(out, qPoints, qLimit)
	if e.v.Type == exam.TypeSingle || e.v.Type == exam.TypeMultiple {
		out = append(out, qReveal)
	}
	return append(out, qLens)
}

// sync copies the fields to the values, leaving out empty options.
func (e *questionEdit) sync() {
	v := e.v
	v.Prompt, v.Points, v.TimeLimit = e.prompt.value(), e.points.value(), e.limit.value()
	v.Number, v.Tolerance, v.Accept = e.number.value(), e.tolerance.value(), e.accept.value()
	var texts []string
	v.Correct, v.CorrectM = -1, nil
	for i := range e.opts {
		t := strings.TrimSpace(e.opts[i].value())
		if t == "" {
			continue
		}
		if e.marks[i] {
			v.Correct = len(texts)
			v.CorrectM = append(v.CorrectM, len(texts))
		}
		texts = append(texts, t)
	}
	v.Options = strings.Join(texts, "\n")
}

func (e *questionEdit) blank() bool {
	return strings.TrimSpace(e.v.Prompt) == "" && e.v.Options == "" && strings.TrimSpace(e.v.Number) == "" &&
		strings.TrimSpace(e.v.Accept) == "" && e.v.Value == nil
}

func (e *questionEdit) paste(s string) {
	switch e.field {
	case qPrompt:
		e.prompt.insert(s)
	case qAccept:
		e.accept.insert(s)
	case qOptions: // a pasted list becomes one option per line
		for _, l := range lines(s) {
			for e.opt < len(e.opts) && e.opts[e.opt].value() != "" {
				e.opt++
			}
			if e.opt == len(e.opts) {
				e.opt--
				return
			}
			e.opts[e.opt].insert(l)
			e.addRow()
		}
		e.opt = min(e.opt, len(e.opts)-1)
	default:
		if f := e.single(); f != nil {
			f.insert(strings.ReplaceAll(s, "\n", " "))
		}
	}
}

func (e *questionEdit) single() *textField {
	switch e.field {
	case qPoints:
		return &e.points
	case qLimit:
		return &e.limit
	case qNumber:
		return &e.number
	case qTolerance:
		return &e.tolerance
	}
	return nil
}

func (e *questionEdit) move(d int) {
	f := e.fields()
	at := max(0, slices.Index(f, e.field))
	e.field = f[min(max(0, at+d), len(f)-1)]
	e.typing = false
}

// turn moves the choice of a chip row.
func (e *questionEdit) turn(d int) {
	cycle := func(list []string, cur string) string {
		return list[(max(0, slices.Index(list, cur))+d+len(list))%len(list)]
	}
	v := e.v
	switch e.field {
	case qType:
		v.Type = cycle(questionTypes, v.Type)
	case qPartial:
		v.Partial = !v.Partial
	case qValue:
		on := v.Value == nil || !*v.Value // the first press lands on True
		v.Value = &on
	case qInput:
		v.Input = cycle([]string{"number", "text"}, v.Input)
	case qReveal:
		v.Reveal = cycle(revealModes, v.Reveal)
	case qLens:
		v.Lens = cycle(lensModes, v.Lens)
	}
}

// mark sets the right option: one for single choice, any for multiple.
func (e *questionEdit) mark() {
	if e.opts[e.opt].value() == "" {
		return
	}
	on := !e.marks[e.opt]
	if e.v.Type == exam.TypeSingle {
		clear(e.marks)
	}
	e.marks[e.opt] = on
}

func (e *questionEdit) optionsKey(k tea.KeyPressMsg) {
	s := k.String()
	f := &e.opts[e.opt]
	switch {
	case s == "up" && e.opt == 0:
		e.move(-1)
	case s == "up":
		e.opt--
		e.typing = false
	case s == "down" && e.opt == len(e.opts)-1:
		e.move(1)
	case s == "down":
		e.opt++
		e.typing = false
	case s == "enter" && e.typing:
		e.addRow()
		e.typing = e.opt+1 < len(e.opts) && f.value() != ""
		e.opt = min(e.opt+1, len(e.opts)-1)
	case s == "enter":
		e.typing = true
	case !e.typing && (s == " " || s == "space"):
		e.mark()
	case !e.typing && s == "delete":
		if e.opt < len(e.opts)-1 || f.value() != "" {
			e.opts = slices.Delete(e.opts, e.opt, e.opt+1)
			e.marks = slices.Delete(e.marks, e.opt, e.opt+1)
			e.addRow()
			e.opt = min(e.opt, len(e.opts)-1)
		}
	default:
		if f.key(k, false) && s != "left" && s != "right" {
			e.typing = true
			e.addRow()
		}
	}
}

func (e *questionEdit) key(k tea.KeyPressMsg) {
	s := k.String()
	switch e.field {
	case qOptions:
		e.optionsKey(k)
		return
	case qPrompt:
		if e.prompt.key(k, true) {
			return
		}
	case qAccept:
		if e.accept.key(k, true) {
			return
		}
	case qPoints, qLimit, qNumber, qTolerance:
		if s == "enter" {
			e.move(1)
			return
		}
		if e.single().key(k, false) {
			return
		}
	}
	switch s {
	case "up":
		e.move(-1)
	case "down", "enter":
		e.move(1)
	case "left":
		e.turn(-1)
	case "right":
		e.turn(1)
	}
}

// saveQuestion writes the question and keeps the form open on it. An untouched
// new question writes nothing.
func (m *Editor) saveQuestion() bool {
	e := m.q
	e.sync()
	if e.isNew && e.blank() {
		return true
	}
	q, a, err := e.v.build()
	if err == nil {
		err = m.d.SaveQuestion(q, a)
	}
	if err != nil {
		m.fail(err)
		return false
	}
	e.isNew, e.v.old, e.v.oldKey = false, m.d.Exam.Questions[q.ID], a
	m.selectQuestion(q.ID)
	m.markSaved()
	return true
}

func (m *Editor) writingKey(k tea.KeyPressMsg) tea.Cmd {
	e := m.q
	switch k.String() {
	case "esc":
		if m.saveQuestion() {
			m.q = nil
		}
	case "tab":
		if m.saveQuestion() {
			e.move(1)
		}
	case "shift+tab":
		if m.saveQuestion() {
			e.move(-1)
		}
	case "ctrl+e":
		e.sync()
		if e.isNew && e.blank() {
			m.fail(errors.New(T("editor.ui.write_first")))
			return nil
		}
		if !m.saveQuestion() {
			return nil
		}
		m.q = nil
		if row, ok := m.selected(); ok {
			return m.external(row)
		}
	default:
		e.key(k)
	}
	return nil
}
