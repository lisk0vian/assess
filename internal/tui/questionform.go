package tui

import (
	"errors"
	"fmt"
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
		Partial: a.Partial, Input: q.Input, Reveal: q.Reveal, old: q, oldKey: a}
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
		return fmt.Errorf("escribe un número mayor que 0 y hasta %d", exam.MaxPoints)
	}
	return nil
}

func checkNumber(s string) error {
	if _, err := parseNum(s); err != nil {
		return errors.New("escribe un número, por ejemplo 4 o 2,5")
	}
	return nil
}

func checkOptionalNumber(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if v, err := parseNum(s); err != nil || v < 0 {
		return errors.New("escribe un número positivo o déjalo vacío")
	}
	return nil
}

func checkTimeLimit(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < exam.MinTimeLimit {
		return errors.New("usa una duración como 90s o 2m (mínimo 5s), o déjalo vacío")
	}
	return nil
}

func checkOptions(s string) error {
	switch n := len(lines(s)); {
	case n < exam.MinOptions:
		return fmt.Errorf("escribe al menos %d opciones, una por línea", exam.MinOptions)
	case n > exam.MaxOptions:
		return fmt.Errorf("como mucho %d opciones", exam.MaxOptions)
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
	var a exam.Answer
	pts, err := parseNum(v.Points)
	if err != nil {
		return nil, a, errors.New("puntos: escribe un número")
	}
	a.Points = pts
	if tl := strings.TrimSpace(v.TimeLimit); tl != "" {
		d, err := time.ParseDuration(tl)
		if err != nil {
			return nil, a, errors.New("tiempo límite: usa una duración como 2m")
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
				return nil, a, errors.New("respuesta: escribe un número")
			}
			a.Number = &n
			if strings.TrimSpace(v.Tolerance) != "" {
				t, err := parseNum(v.Tolerance)
				if err != nil {
					return nil, a, errors.New("tolerancia: escribe un número")
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

var typeOptions = []huh.Option[string]{
	huh.NewOption("Una opción correcta", exam.TypeSingle),
	huh.NewOption("Varias opciones correctas", exam.TypeMultiple),
	huh.NewOption("Verdadero o falso", exam.TypeTrueFalse),
	huh.NewOption("Respuesta corta (número o texto)", exam.TypeShort),
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
		head = append(head, huh.NewSelect[string]().Title("Tipo de pregunta").Options(typeOptions...).Value(&v.Type))
	}
	if isNew && taken != nil {
		head = append(head,
			huh.NewInput().Title("Identificador").Description("También es el nombre del archivo: questions/<id>.yaml").Value(&v.ID).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					switch {
					case !exam.ValidID(s):
						return errors.New("solo minúsculas, números, - y _")
					case taken(s):
						return errors.New("ya existe una pregunta con ese identificador")
					}
					return nil
				}))
	}
	if !isNew {
		head = append(head, huh.NewNote().Title(v.ID+" · "+typeName(v.Type)).Description("El tipo y el identificador no se cambian aquí; usa E para editar el archivo."))
	}
	head = append(head,
		huh.NewText().Title("Enunciado").Description("Markdown y fórmulas entre $…$. Alt+Enter o Ctrl+J para saltar de línea.").
			Lines(6).CharLimit(exam.MaxMarkdownBytes).Value(&v.Prompt).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("escribe el enunciado")
				}
				return nil
			}),
		huh.NewInput().Title("Puntos").Value(&v.Points).Validate(checkPoints),
	)

	return huh.NewForm(
		huh.NewGroup(head...),
		huh.NewGroup(
			huh.NewText().Title("Opciones").Description("Una por línea, en el orden en que se escriben (se pueden barajar al alumno).").
				Lines(6).Value(&v.Options).Validate(checkOptions),
		).WithHideFunc(is(exam.TypeSingle, exam.TypeMultiple)),
		huh.NewGroup(
			huh.NewSelect[int]().Title("¿Cuál es la correcta?").OptionsFunc(v.optionChoices, &v.Options).Value(&v.Correct),
		).WithHideFunc(is(exam.TypeSingle)),
		huh.NewGroup(
			huh.NewMultiSelect[int]().Title("¿Cuáles son correctas?").Description("Espacio marca o desmarca.").
				OptionsFunc(v.optionChoices, &v.Options).Value(&v.CorrectM).
				Validate(func(s []int) error {
					if len(s) == 0 {
						return errors.New("marca al menos una")
					}
					return nil
				}),
			huh.NewConfirm().Title("¿Puntuación parcial?").Description("Sí: cada acierto suma su parte. No: todo o nada.").
				Affirmative("Sí").Negative("No").Value(&v.Partial),
		).WithHideFunc(is(exam.TypeMultiple)),
		huh.NewGroup(
			huh.NewConfirm().Title("La afirmación es…").Affirmative("Verdadera").Negative("Falsa").Value(&v.Value),
		).WithHideFunc(is(exam.TypeTrueFalse)),
		huh.NewGroup(
			huh.NewSelect[string]().Title("El alumno escribe…").Options(
				huh.NewOption("Un número", "number"), huh.NewOption("Un texto corto", "text")).Value(&v.Input),
		).WithHideFunc(is(exam.TypeShort)),
		huh.NewGroup(
			huh.NewInput().Title("Respuesta correcta").Value(&v.Number).Validate(checkNumber),
			huh.NewInput().Title("Tolerancia").Description("Diferencia aceptada, por ejemplo 0,01. Vacío: exacta.").
				Value(&v.Tolerance).Validate(checkOptionalNumber),
		).WithHideFunc(func() bool { return v.Type != exam.TypeShort || v.Input != "number" }),
		huh.NewGroup(
			huh.NewText().Title("Respuestas aceptadas").Description("Una por línea. Se ignoran mayúsculas, tildes y espacios de más.").
				Lines(4).Value(&v.Accept).Validate(func(s string) error {
				if len(lines(s)) == 0 {
					return errors.New("escribe al menos una respuesta")
				}
				return nil
			}),
		).WithHideFunc(func() bool { return v.Type != exam.TypeShort || v.Input != "text" }),
		huh.NewGroup(
			huh.NewInput().Title("Tiempo límite de la pregunta").Description("Opcional, por ejemplo 2m. Al agotarse se cierra.").
				Value(&v.TimeLimit).Validate(checkTimeLimit),
			huh.NewSelect[string]().Title("Mostrar las opciones").Options(
				huh.NewOption("Como diga el examen", ""), huh.NewOption("Siempre", "always"),
				huh.NewOption("Al pasar el ratón", "hover")).Value(&v.Reveal),
		).WithHideFunc(is(exam.TypeSingle, exam.TypeMultiple)),
		huh.NewGroup(
			huh.NewInput().Title("Tiempo límite de la pregunta").Description("Opcional, por ejemplo 2m. Al agotarse se cierra.").
				Value(&v.TimeLimit).Validate(checkTimeLimit),
		).WithHideFunc(is(exam.TypeTrueFalse, exam.TypeShort)),
	)
}

// manifestValues are the fields of the exam settings form.
type manifestValues struct {
	Title, Duration, Language, Visibility, Instructions string
	ShuffleQ, ShuffleO                                  bool
	Profile, StrikeLimit                                string
	m                                                   exam.Manifest
}

func manifestValuesOf(m exam.Manifest) *manifestValues {
	v := &manifestValues{Title: m.Title, Language: m.Lang(), Visibility: m.Visibility(),
		Instructions: strings.TrimRight(m.Instructions, "\n"), ShuffleQ: m.ShuffleQuestions(), ShuffleO: m.ShuffleOptions(),
		Profile: m.Profile(), m: m}
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
		return m, errors.New("duración: usa por ejemplo 45m o 1h30m")
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
	m.Proctoring.StrikeLimit = 0
	if s := strings.TrimSpace(v.StrikeLimit); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return m, errors.New("strikes: escribe un número entero")
		}
		m.Proctoring.StrikeLimit = n
	}
	return m, nil
}

func (v *manifestValues) form() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Ajustes del examen").Description("Identificador: "+v.m.ID+" (no se cambia: enlaza el historial de sesiones)"),
			huh.NewInput().Title("Título").Value(&v.Title).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("escribe un título")
				}
				return nil
			}),
			huh.NewInput().Title("Duración").Description("Por ejemplo 45m o 1h30m.").Value(&v.Duration).Validate(func(s string) error {
				d, err := time.ParseDuration(strings.TrimSpace(s))
				if err != nil || d < time.Minute || d > exam.MaxDuration {
					return errors.New("usa una duración entre 1m y 24h, como 45m")
				}
				return nil
			}),
			huh.NewSelect[string]().Title("Idioma de la página del alumno").Options(
				huh.NewOption("Español", "es"), huh.NewOption("English", "en")).Value(&v.Language),
			huh.NewSelect[string]().Title("Al entregar, el alumno ve…").Options(
				huh.NewOption("Nada", "none"), huh.NewOption("Su nota", "score"),
				huh.NewOption("Su nota y la corrección (al cerrar la sesión)", "review")).Value(&v.Visibility),
		),
		huh.NewGroup(
			huh.NewText().Title("Instrucciones").Description("Se muestran en la sala de espera. Markdown.").Lines(6).Value(&v.Instructions),
			huh.NewConfirm().Title("¿Barajar el orden de las preguntas?").Affirmative("Sí").Negative("No").Value(&v.ShuffleQ),
			huh.NewConfirm().Title("¿Barajar el orden de las opciones?").Affirmative("Sí").Negative("No").Value(&v.ShuffleO),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Vigilancia").Options(
				huh.NewOption("Estándar: avisa la primera vez, 3 strikes anulan", "standard"),
				huh.NewOption("Estricta: sin margen, 2 strikes anulan", "strict"),
				huh.NewOption("Desactivada", "off")).Value(&v.Profile),
			huh.NewInput().Title("Strikes para anular").Description("Vacío: los del perfil.").Value(&v.StrikeLimit).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return nil
					}
					if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 1 || n > 10 {
						return errors.New("entre 1 y 10, o vacío")
					}
					return nil
				}),
		),
	)
}

// formKeys is huh's key map with the help in Spanish, and S for "sí".
func formKeys() *huh.KeyMap {
	k := huh.NewDefaultKeyMap()
	help := func(b *key.Binding, desc string) { b.SetHelp(b.Help().Key, desc) }
	for _, b := range []*key.Binding{&k.Input.Prev, &k.Text.Prev, &k.Select.Prev, &k.MultiSelect.Prev, &k.Note.Prev, &k.Confirm.Prev} {
		help(b, "atrás")
	}
	for _, b := range []*key.Binding{&k.Input.Next, &k.Text.Next, &k.Note.Next, &k.Confirm.Next} {
		help(b, "siguiente")
	}
	for _, b := range []*key.Binding{&k.Input.Submit, &k.Text.Submit, &k.Select.Submit, &k.MultiSelect.Submit, &k.Note.Submit, &k.Confirm.Submit} {
		help(b, "guardar")
	}
	help(&k.Select.Next, "elegir")
	help(&k.MultiSelect.Next, "confirmar")
	help(&k.Text.NewLine, "nueva línea")
	help(&k.Text.Editor, "abrir en tu editor")
	help(&k.Input.AcceptSuggestion, "completar")
	for _, b := range []*key.Binding{&k.Select.Up, &k.MultiSelect.Up} {
		help(b, "arriba")
	}
	for _, b := range []*key.Binding{&k.Select.Down, &k.MultiSelect.Down} {
		help(b, "abajo")
	}
	for _, b := range []*key.Binding{&k.Select.Filter, &k.MultiSelect.Filter} {
		help(b, "filtrar")
	}
	help(&k.Select.GotoTop, "inicio")
	help(&k.Select.GotoBottom, "final")
	help(&k.MultiSelect.GotoTop, "inicio")
	help(&k.MultiSelect.GotoBottom, "final")
	help(&k.Select.HalfPageUp, "½ página arriba")
	help(&k.Select.HalfPageDown, "½ página abajo")
	help(&k.MultiSelect.HalfPageUp, "½ página arriba")
	help(&k.MultiSelect.HalfPageDown, "½ página abajo")
	k.MultiSelect.Toggle.SetHelp("espacio", "marcar")
	help(&k.MultiSelect.SelectAll, "marcar todas")
	help(&k.MultiSelect.SelectNone, "desmarcar todas")
	help(&k.Confirm.Toggle, "cambiar")
	k.Confirm.Accept.SetKeys("y", "Y", "s", "S")
	k.Confirm.Accept.SetHelp("s", "sí")
	k.Confirm.Reject.SetHelp("n", "no")
	return k
}

// StyleForm gives a form the panel's colours and Spanish key help.
func StyleForm(f *huh.Form) *huh.Form {
	return f.WithTheme(huh.ThemeFunc(formTheme)).WithKeyMap(formKeys())
}
