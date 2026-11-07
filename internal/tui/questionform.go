package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/lisk0vian/assess/internal/exam"
)

// questionValues are the fields of the question form, as the form edits
// them: options and accepted answers are one per line, numbers are text.
type questionValues struct {
	ID        string
	Type      string
	Prompt    string
	Points    string
	Options   string // one option per line
	Correct   int    // single: line index of the right option, -1 when not marked
	CorrectM  []int  // multiple: line indexes of the right options
	Partial   bool
	Value     *bool // truefalse: nil until the teacher says
	Input     string
	Number    string
	Tolerance string
	Accept    string // one accepted answer per line
	TimeLimit string
	Reveal    string // "" inherits the exam's setting
	Lens      string // "" inherits the exam's setting, "on" or "off"

	old    *exam.Question // nil for a new question
	oldKey exam.Answer
}

// defaultNormalize is what new text answers ignore: spacing, case, accents.
var defaultNormalize = []string{"trim", "casefold", "collapse_spaces", "strip_accents"}

func newQuestionValues(id string) *questionValues {
	return &questionValues{ID: id, Type: exam.TypeSingle, Points: "1", Input: "number", Correct: -1}
}

func questionValuesOf(q *exam.Question, a exam.Answer) *questionValues {
	v := &questionValues{ID: q.ID, Type: q.Type, Prompt: strings.TrimRight(q.Prompt, "\n"), Points: fmtNum(a.Points),
		Partial: a.Partial, Input: q.Input, Reveal: q.Reveal, Lens: onOff(q.Lens), Correct: -1, old: q, oldKey: a}
	if v.Input == "" {
		v.Input = "number"
	}
	var lines []string
	for i, o := range q.Options {
		lines = append(lines, o.Text)
		if o.ID == a.Option {
			v.Correct = i
		}
		if slices.Contains(a.Options, o.ID) {
			v.CorrectM = append(v.CorrectM, i)
		}
	}
	v.Options = strings.Join(lines, "\n")
	v.Value = a.Value
	if a.Number != nil {
		v.Number = fmtNum(*a.Number)
	}
	if a.Tolerance != nil {
		v.Tolerance = fmtNum(*a.Tolerance)
	}
	v.Accept = strings.Join(a.Accept, "\n")
	if q.TimeLimit != 0 {
		v.TimeLimit = exam.FormatDuration(q.TimeLimit.D())
	}
	return v
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// lines splits text into its non-empty, trimmed lines.
func lines(s string) []string {
	var out []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// parseNum accepts a decimal comma too.
func parseNum(s string) (float64, error) {
	return strconv.ParseFloat(strings.Replace(strings.TrimSpace(s), ",", ".", 1), 64)
}

// optionIDs gives each option line an id: the old option at that position
// keeps its id (and pin); new lines get the next free letter.
func (v *questionValues) optionIDs(n int) []exam.Option {
	used := map[string]bool{}
	out := make([]exam.Option, n)
	for i := range out {
		if v.old != nil && i < len(v.old.Options) {
			out[i] = exam.Option{ID: v.old.Options[i].ID, Pin: v.old.Options[i].Pin}
			used[out[i].ID] = true
		}
	}
	next := 'a'
	for i := range out {
		if out[i].ID != "" {
			continue
		}
		for used[string(next)] {
			next++
		}
		out[i].ID = string(next)
		used[out[i].ID] = true
	}
	return out
}

// build turns the form values into a question and its answer. Fields that
// do not apply to the chosen type are dropped.
func (v *questionValues) build() (*exam.Question, exam.Answer, error) {
	q := &exam.Question{ID: strings.TrimSpace(v.ID), Type: v.Type, Prompt: strings.TrimSpace(v.Prompt)}
	if v.old != nil {
		if v.old.Type == v.Type {
			q.Placeholder = v.old.Placeholder
		}
		if strings.TrimSpace(v.old.Prompt) == q.Prompt {
			q.Prompt = v.old.Prompt // unchanged: keep it as written, block style included
		}
	}
	q.Lens = fromOnOff(v.Lens)
	var a exam.Answer
	pts, err := parseNum(v.Points)
	if err != nil {
		return nil, a, errors.New(T("form.err.points_field"))
	}
	a.Points = pts
	if tl := strings.TrimSpace(v.TimeLimit); tl != "" {
		d, err := time.ParseDuration(tl)
		if err != nil {
			return nil, a, errors.New(T("form.err.time_limit_field"))
		}
		q.TimeLimit = exam.Duration(d)
	}

	switch v.Type {
	case exam.TypeSingle, exam.TypeMultiple:
		texts := lines(v.Options)
		q.Options = v.optionIDs(len(texts))
		for i, t := range texts {
			q.Options[i].Text = t
		}
		q.Reveal = v.Reveal
		if v.Type == exam.TypeSingle {
			if v.Correct >= 0 && v.Correct < len(q.Options) {
				a.Option = q.Options[v.Correct].ID
			}
		} else {
			for _, i := range slices.Sorted(slices.Values(v.CorrectM)) {
				if i >= 0 && i < len(q.Options) {
					a.Options = append(a.Options, q.Options[i].ID)
				}
			}
			a.Partial = v.Partial
		}
	case exam.TypeTrueFalse:
		a.Value = v.Value
	case exam.TypeShort:
		q.Input = v.Input
		if q.Input == "number" {
			if strings.TrimSpace(v.Number) != "" { // empty is allowed: Review asks for it
				n, err := parseNum(v.Number)
				if err != nil {
					return nil, a, errors.New(T("form.err.answer_field"))
				}
				a.Number = &n
			}
			if strings.TrimSpace(v.Tolerance) != "" {
				t, err := parseNum(v.Tolerance)
				if err != nil {
					return nil, a, errors.New(T("form.err.tolerance_field"))
				}
				a.Tolerance = &t
			}
		} else {
			a.Accept = lines(v.Accept)
			a.Normalize = v.oldKey.Normalize
			if a.Normalize == nil {
				a.Normalize = defaultNormalize
			}
		}
	}
	return q, a, nil
}

// onOff and fromOnOff convert an optional bool to the "", "on", "off" of a select.
func onOff(b *bool) string {
	switch {
	case b == nil:
		return ""
	case *b:
		return "on"
	}
	return "off"
}

func fromOnOff(s string) *bool {
	if s == "" {
		return nil
	}
	b := s == "on"
	return &b
}

// manifestValues are the fields of the exam settings form.
type manifestValues struct {
	Title, Duration, Language, Visibility, Instructions string
	ShuffleQ, ShuffleO                                  bool
	Profile, StrikeLimit, ReadSpeed                     string
	Lens, FrozenPointer, AutoRead                       bool
	m                                                   exam.Manifest
}

func manifestValuesOf(m exam.Manifest) *manifestValues {
	v := &manifestValues{Title: m.Title, Language: m.Lang(), Visibility: m.Visibility(),
		Instructions: strings.TrimRight(m.Instructions, "\n"), ShuffleQ: m.ShuffleQuestions(), ShuffleO: m.ShuffleOptions(),
		Profile: m.Profile(), m: m}
	on := func(b *bool) bool { return b == nil || *b } // the photo protections default to on
	v.Lens, v.FrozenPointer = on(m.Proctoring.Lens), on(m.Proctoring.FrozenPointer)
	v.AutoRead = on(m.Proctoring.AutoRead)
	v.ReadSpeed = m.Proctoring.ReadSpeed
	if v.ReadSpeed == "" {
		v.ReadSpeed = "normal"
	}
	if m.Duration != 0 {
		v.Duration = exam.FormatDuration(m.Duration.D())
	}
	if m.Proctoring.StrikeLimit > 0 {
		v.StrikeLimit = strconv.Itoa(m.Proctoring.StrikeLimit)
	}
	return v
}

// build applies the values to the manifest, writing optional settings only
// when they differ from the defaults or were already written.
func (v *manifestValues) build() (exam.Manifest, error) {
	m := v.m
	m.Title = strings.TrimSpace(v.Title)
	d, err := time.ParseDuration(strings.TrimSpace(v.Duration))
	if err != nil {
		return m, errors.New(T("form.err.duration_field"))
	}
	m.Duration = exam.Duration(d)
	if m.Instructions = strings.TrimSpace(v.Instructions); m.Instructions == strings.TrimSpace(v.m.Instructions) {
		m.Instructions = v.m.Instructions
	}
	setDefault := func(field *string, value, def string) {
		if *field != "" || value != def {
			*field = value
		}
	}
	setDefault(&m.Language, v.Language, exam.DefaultLanguage)
	setDefault(&m.ResultVisibility, v.Visibility, exam.DefaultResultVisibility)
	setDefault(&m.Proctoring.Profile, v.Profile, exam.DefaultProfile)
	setBool := func(field **bool, value bool) {
		if *field != nil || !value {
			b := value
			*field = &b
		}
	}
	setBool(&m.Order.ShuffleQuestions, v.ShuffleQ)
	setBool(&m.Order.ShuffleOptions, v.ShuffleO)
	setBool(&m.Proctoring.Lens, v.Lens)
	setBool(&m.Proctoring.FrozenPointer, v.FrozenPointer)
	setBool(&m.Proctoring.AutoRead, v.AutoRead)
	setDefault(&m.Proctoring.ReadSpeed, v.ReadSpeed, "normal")
	m.Proctoring.StrikeLimit = 0
	if s := strings.TrimSpace(v.StrikeLimit); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return m, errors.New(T("form.err.strikes_field"))
		}
		m.Proctoring.StrikeLimit = n
	}
	return m, nil
}

// formKeys is huh's key map with translated help, and the first letter of
// the translated "yes" accepting confirmations.
func formKeys() *huh.KeyMap {
	k := huh.NewDefaultKeyMap()
	help := func(b *key.Binding, desc string) { b.SetHelp(b.Help().Key, desc) }
	for _, b := range []*key.Binding{&k.Input.Prev, &k.Text.Prev, &k.Select.Prev, &k.MultiSelect.Prev, &k.Note.Prev, &k.Confirm.Prev} {
		help(b, T("keys.back"))
	}
	for _, b := range []*key.Binding{&k.Input.Next, &k.Text.Next, &k.Note.Next, &k.Confirm.Next} {
		help(b, T("keys.next"))
	}
	for _, b := range []*key.Binding{&k.Input.Submit, &k.Text.Submit, &k.Select.Submit, &k.MultiSelect.Submit, &k.Note.Submit, &k.Confirm.Submit} {
		help(b, T("keys.save"))
	}
	help(&k.Select.Next, T("keys.choose"))
	help(&k.MultiSelect.Next, T("keys.confirm"))
	help(&k.Text.NewLine, T("keys.newline"))
	help(&k.Text.Editor, T("keys.editor"))
	help(&k.Input.AcceptSuggestion, T("keys.complete"))
	for _, b := range []*key.Binding{&k.Select.Up, &k.MultiSelect.Up} {
		help(b, T("keys.up"))
	}
	for _, b := range []*key.Binding{&k.Select.Down, &k.MultiSelect.Down} {
		help(b, T("keys.down"))
	}
	for _, b := range []*key.Binding{&k.Select.Filter, &k.MultiSelect.Filter} {
		help(b, T("keys.filter"))
	}
	help(&k.Select.GotoTop, T("keys.top"))
	help(&k.Select.GotoBottom, T("keys.bottom"))
	help(&k.MultiSelect.GotoTop, T("keys.top"))
	help(&k.MultiSelect.GotoBottom, T("keys.bottom"))
	help(&k.Select.HalfPageUp, T("keys.half_up"))
	help(&k.Select.HalfPageDown, T("keys.half_down"))
	help(&k.MultiSelect.HalfPageUp, T("keys.half_up"))
	help(&k.MultiSelect.HalfPageDown, T("keys.half_down"))
	k.MultiSelect.Toggle.SetHelp(T("keys.space"), T("keys.mark"))
	help(&k.MultiSelect.SelectAll, T("keys.mark_all"))
	help(&k.MultiSelect.SelectNone, T("keys.mark_none"))
	help(&k.Confirm.Toggle, T("keys.toggle"))
	yes, no := yesNoKeys()
	k.Confirm.Accept.SetKeys(yes...)
	k.Confirm.Accept.SetHelp(yes[0], T("common.yes_short"))
	k.Confirm.Reject.SetKeys(no...)
	k.Confirm.Reject.SetHelp(no[0], T("common.no_short"))
	return k
}

// StyleForm gives a form the panel's colours and translated key help.
func StyleForm(f *huh.Form) *huh.Form {
	return f.WithTheme(huh.ThemeFunc(formTheme)).WithKeyMap(formKeys())
}

// formTheme is huh's base theme in the panel's colours.
func formTheme(isDark bool) *huh.Styles {
	t := huh.ThemeCharm(isDark)
	t.Focused.Title = t.Focused.Title.Foreground(accent)
	t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(accent)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(accent)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(accent)
	t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(accent)
	t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(accent)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(accent)
	t.Focused.Next = t.Focused.FocusedButton
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(accent)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	t.Group.Title = t.Focused.Title
	return t
}
