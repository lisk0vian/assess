package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/huh/v2"

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
	Correct   int    // single: line index of the right option
	CorrectM  []int  // multiple: line indexes of the right options
	Partial   bool
	Value     bool // truefalse
	Input     string
	Number    string
	Tolerance string
	Accept    string // one accepted answer per line
	TimeLimit string
	Reveal    string // "" inherits the exam's setting
	Lens      string // "" inherits the exam's setting, "on" or "off"

	old    *exam.Question // nil for a new question
	oldKey exam.Answer
	// marked are the right options when the form opened. Huh clears the
	// bound value while it loads dynamic options, so they are marked from
	// this copy.
	marked []int
}

// defaultNormalize is what new text answers ignore: spacing, case, accents.
var defaultNormalize = []string{"trim", "casefold", "collapse_spaces", "strip_accents"}

func newQuestionValues(id string) *questionValues {
	return &questionValues{ID: id, Type: exam.TypeSingle, Points: "1", Input: "number", Options: "\n"}
}

func questionValuesOf(q *exam.Question, a exam.Answer) *questionValues {
	v := &questionValues{ID: q.ID, Type: q.Type, Prompt: strings.TrimRight(q.Prompt, "\n"), Points: fmtNum(a.Points),
		Partial: a.Partial, Input: q.Input, Reveal: q.Reveal, Lens: onOff(q.Lens), old: q, oldKey: a}
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
			v.marked = append(v.marked, i)
		}
	}
	v.Options = strings.Join(lines, "\n")
	if a.Value != nil {
		v.Value = *a.Value
	}
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

func checkPoints(s string) error {
	v, err := parseNum(s)
	if err != nil || v <= 0 || v > exam.MaxPoints {
		return errors.New(T("form.err.points", "max", exam.MaxPoints))
	}
	return nil
}

func checkNumber(s string) error {
	if _, err := parseNum(s); err != nil {
		return errors.New(T("form.err.number"))
	}
	return nil
}

func checkOptionalNumber(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if v, err := parseNum(s); err != nil || v < 0 {
		return errors.New(T("form.err.optional_number"))
	}
	return nil
}

func checkTimeLimit(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < exam.MinTimeLimit {
		return errors.New(T("form.err.time_limit"))
	}
	return nil
}

func checkOptions(s string) error {
	switch n := len(lines(s)); {
	case n < exam.MinOptions:
		return errors.New(T("form.err.min_options", "n", exam.MinOptions))
	case n > exam.MaxOptions:
		return errors.New(T("form.err.max_options", "n", exam.MaxOptions))
	}
	return nil
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
		q.Placeholder = v.old.Placeholder
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
		val := v.Value
		a.Value = &val
	case exam.TypeShort:
		q.Input = v.Input
		if q.Input == "number" {
			n, err := parseNum(v.Number)
			if err != nil {
				return nil, a, errors.New(T("form.err.answer_field"))
			}
			a.Number = &n
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
		q.Placeholder = ""
		if v.old != nil && v.old.Type == exam.TypeShort {
			q.Placeholder = v.old.Placeholder
		}
	}
	return q, a, nil
}

func typeOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption(T("form.type.single"), exam.TypeSingle),
		huh.NewOption(T("form.type.multiple"), exam.TypeMultiple),
		huh.NewOption(T("form.type.truefalse"), exam.TypeTrueFalse),
		huh.NewOption(T("form.type.short"), exam.TypeShort),
	}
}

// optionChoices lists the option lines for picking the right ones, with
// the current right ones already marked.
func (v *questionValues) optionChoices() []huh.Option[int] {
	var out []huh.Option[int]
	for i, t := range lines(v.Options) {
		out = append(out, huh.NewOption(t, i).Selected(slices.Contains(v.marked, i)))
	}
	return out
}

// form builds the question form. taken reports ids already in use; nil
// means the id is assigned by the caller and not asked.
func (v *questionValues) form(taken func(string) bool) *huh.Form {
	isNew := v.old == nil
	is := func(types ...string) func() bool {
		return func() bool { return !slices.Contains(types, v.Type) }
	}
	var head []huh.Field
	if isNew {
		head = append(head, huh.NewSelect[string]().Title(T("form.type.title")).Options(typeOptions()...).Value(&v.Type))
	}
	if isNew && taken != nil {
		head = append(head,
			huh.NewInput().Title(T("form.id.title")).Description(T("form.id.hint")).Value(&v.ID).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					switch {
					case !exam.ValidID(s):
						return errors.New(T("form.err.id"))
					case taken(s):
						return errors.New(T("form.err.id_taken"))
					}
					return nil
				}))
	}
	if !isNew {
		head = append(head, huh.NewNote().Title(v.ID+" · "+typeName(v.Type)).Description(T("form.id.fixed")))
	}
	head = append(head,
		huh.NewText().Title(T("form.prompt.title")).Description(T("form.prompt.hint")).
			Lines(6).CharLimit(exam.MaxMarkdownBytes).Value(&v.Prompt).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New(T("form.err.prompt"))
				}
				return nil
			}),
		huh.NewInput().Title(T("form.points")).Value(&v.Points).Validate(checkPoints),
	)

	return huh.NewForm(
		huh.NewGroup(head...),
		huh.NewGroup(
			huh.NewText().Title(T("form.options.title")).Description(T("form.options.hint")).
				Lines(6).Value(&v.Options).Validate(checkOptions),
		).WithHideFunc(is(exam.TypeSingle, exam.TypeMultiple)),
		huh.NewGroup(
			huh.NewSelect[int]().Title(T("form.correct_one")).OptionsFunc(v.optionChoices, &v.Options).Value(&v.Correct),
		).WithHideFunc(is(exam.TypeSingle)),
		huh.NewGroup(
			huh.NewMultiSelect[int]().Title(T("form.correct_many")).Description(T("form.correct_many_hint")).
				OptionsFunc(v.optionChoices, &v.Options).Value(&v.CorrectM).
				Validate(func(s []int) error {
					if len(s) == 0 {
						return errors.New(T("form.err.mark_one"))
					}
					return nil
				}),
			huh.NewConfirm().Title(T("form.partial.title")).Description(T("form.partial.hint")).
				Affirmative(T("common.yes")).Negative(T("common.no")).Value(&v.Partial),
		).WithHideFunc(is(exam.TypeMultiple)),
		huh.NewGroup(
			huh.NewConfirm().Title(T("form.statement")).Affirmative(T("form.true")).Negative(T("form.false")).Value(&v.Value),
		).WithHideFunc(is(exam.TypeTrueFalse)),
		huh.NewGroup(
			huh.NewSelect[string]().Title(T("form.input.title")).Options(
				huh.NewOption(T("form.input.number"), "number"), huh.NewOption(T("form.input.text"), "text")).Value(&v.Input),
		).WithHideFunc(is(exam.TypeShort)),
		huh.NewGroup(
			huh.NewInput().Title(T("form.number")).Value(&v.Number).Validate(checkNumber),
			huh.NewInput().Title(T("form.tolerance.title")).Description(T("form.tolerance.hint")).
				Value(&v.Tolerance).Validate(checkOptionalNumber),
		).WithHideFunc(func() bool { return v.Type != exam.TypeShort || v.Input != "number" }),
		huh.NewGroup(
			huh.NewText().Title(T("form.accept.title")).Description(T("form.accept.hint")).
				Lines(4).Value(&v.Accept).Validate(func(s string) error {
				if len(lines(s)) == 0 {
					return errors.New(T("form.err.accept"))
				}
				return nil
			}),
		).WithHideFunc(func() bool { return v.Type != exam.TypeShort || v.Input != "text" }),
		huh.NewGroup(
			huh.NewInput().Title(T("form.time_limit.title")).Description(T("form.time_limit.hint")).
				Value(&v.TimeLimit).Validate(checkTimeLimit),
			huh.NewSelect[string]().Title(T("form.reveal.title")).Options(
				huh.NewOption(T("form.inherit"), ""), huh.NewOption(T("form.reveal.always"), "always"),
				huh.NewOption(T("form.reveal.hover"), "hover")).Value(&v.Reveal),
			lensSelect(v),
		).WithHideFunc(is(exam.TypeSingle, exam.TypeMultiple)),
		huh.NewGroup(
			huh.NewInput().Title(T("form.time_limit.title")).Description(T("form.time_limit.hint")).
				Value(&v.TimeLimit).Validate(checkTimeLimit),
			lensSelect(v),
		).WithHideFunc(is(exam.TypeTrueFalse, exam.TypeShort)),
	)
}

func lensSelect(v *questionValues) *huh.Select[string] {
	return huh.NewSelect[string]().Title(T("form.lens.title")).
		Description(T("form.lens.hint")).Options(inheritOptions()...).Value(&v.Lens)
}

func inheritOptions() []huh.Option[string] {
	return []huh.Option[string]{huh.NewOption(T("form.inherit"), ""), huh.NewOption(T("common.yes"), "on"), huh.NewOption(T("common.no"), "off")}
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

func (v *manifestValues) form() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title(T("form.manifest.title")).Description(T("form.manifest.id", "id", v.m.ID)),
			huh.NewInput().Title(T("form.manifest.exam_title")).Value(&v.Title).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New(T("form.err.title"))
				}
				return nil
			}),
			huh.NewInput().Title(T("form.manifest.duration")).Description(T("form.manifest.duration_hint")).Value(&v.Duration).Validate(func(s string) error {
				d, err := time.ParseDuration(strings.TrimSpace(s))
				if err != nil || d < time.Minute || d > exam.MaxDuration {
					return errors.New(T("form.err.duration"))
				}
				return nil
			}),
			huh.NewSelect[string]().Title(T("form.manifest.language")).Options(languageOptions(v.Language)...).Value(&v.Language),
			huh.NewSelect[string]().Title(T("form.manifest.visibility")).Options(visibilityOptions()...).Value(&v.Visibility),
		),
		huh.NewGroup(
			huh.NewText().Title(T("form.manifest.instructions")).Description(T("form.manifest.instructions_hint")).Lines(6).Value(&v.Instructions),
			huh.NewConfirm().Title(T("form.manifest.shuffle_q")).Affirmative(T("common.yes")).Negative(T("common.no")).Value(&v.ShuffleQ),
			huh.NewConfirm().Title(T("form.manifest.shuffle_o")).Affirmative(T("common.yes")).Negative(T("common.no")).Value(&v.ShuffleO),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title(T("form.manifest.proctoring")).Options(profileOptions()...).Value(&v.Profile),
			huh.NewInput().Title(T("form.manifest.strikes")).Description(T("form.manifest.strikes_hint")).Value(&v.StrikeLimit).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return nil
					}
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 || n > 10 {
						return errors.New(T("form.err.strikes"))
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewNote().Title(T("form.manifest.photos")).Description(T("form.manifest.photos_hint")),
			huh.NewConfirm().Title(T("form.manifest.lens")).Description(T("form.manifest.lens_hint")).
				Affirmative(T("common.yes")).Negative(T("common.no")).Value(&v.Lens),
			huh.NewConfirm().Title(T("form.manifest.auto_read")).Description(T("form.manifest.auto_read_hint")).
				Affirmative(T("common.yes")).Negative(T("common.no")).Value(&v.AutoRead),
			huh.NewSelect[string]().Title(T("form.manifest.read_speed")).Description(T("form.manifest.read_speed_hint")).
				Options(readSpeedOptions()...).Value(&v.ReadSpeed),
			huh.NewConfirm().Title(T("form.manifest.frozen")).Description(T("form.manifest.frozen_hint")).
				Affirmative(T("common.yes")).Negative(T("common.no")).Value(&v.FrozenPointer),
		).WithHideFunc(func() bool { return v.Profile == "off" }),
	)
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
