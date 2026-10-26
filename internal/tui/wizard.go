package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
)

// Steps of the wizard.
const (
	stepDetails = iota
	stepQuestions
	stepProctoring
	stepReview
	stepDone
)

var stepKeys = []string{"wizard.step.details", "wizard.step.questions", "wizard.step.proctoring", "wizard.step.review"}

// What to do once the exam is created.
const (
	NextStart = "start"
	NextEdit  = "edit"
)

// Wizard guides a teacher through a new exam without reading the format:
// the details, the questions with a live preview of each one, how it is
// proctored, and a review that validates the exam before anything is
// written to disk.
type Wizard struct {
	step          int
	width, height int

	// The exam being built.
	title, folder, lang, duration, otherDuration string
	visibility, instructions, profile            string
	shuffleQ, shuffleO                           bool
	lens, frozenPointer, autoRead                bool   // against photos of the screen
	readSpeed                                    string // slow | normal | fast
	defaultInstr                                 string // used when instructions are left empty
	questions                                    []*wizQuestion

	form     *huh.Form
	onSubmit func() tea.Cmd
	onEsc    func() tea.Cmd

	list    list.Model
	preview viewport.Model
	help    help.Model
	spin    spinner.Model

	notice  string
	isErr   bool
	confirm string
	onYes   func() tea.Cmd

	busy     string // what is running in the background, with the spinner
	problems exam.Problems
	checked  bool

	// Dir is the folder of the created exam, empty if none was created.
	Dir string
	// Next is NextStart, NextEdit or "" once the wizard closes.
	Next string
}

type wizQuestion struct {
	q *exam.Question
	a exam.Answer
}

func (w wizQuestion) FilterValue() string { return w.q.Prompt }

type (
	checkedMsg struct {
		probs exam.Problems
		err   error
	}
	createdMsg struct{ err error }
)

// wizardKeys are the keys of the questions and review steps.
type wizardKeys struct {
	Add, Edit, Delete, Up, Down, Next, Back, Create, Help, Quit key.Binding
	step                                                        int
}

func newWizardKeys() *wizardKeys {
	return &wizardKeys{
		Add:    key.NewBinding(key.WithKeys("a", "A", "n", "N"), key.WithHelp("a", T("wizard.key.add"))),
		Edit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", T("editor.key.edit"))),
		Delete: key.NewBinding(key.WithKeys("d", "D", "delete"), key.WithHelp("d", T("editor.key.delete"))),
		Up:     key.NewBinding(key.WithKeys("shift+up", "K"), key.WithHelp("⇧↑", T("wizard.key.up"))),
		Down:   key.NewBinding(key.WithKeys("shift+down", "J"), key.WithHelp("⇧↓", T("wizard.key.down"))),
		Next:   key.NewBinding(key.WithKeys("tab", "right"), key.WithHelp("tab", T("wizard.key.next"))),
		Back:   key.NewBinding(key.WithKeys("shift+tab", "left", "esc"), key.WithHelp("⇧tab", T("wizard.key.back"))),
		Create: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", T("wizard.key.create"))),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", T("wizard.key.help"))),
		Quit:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", T("editor.key.quit"))),
	}
}

// ShortHelp is the one-line help of the current step.
func (k *wizardKeys) ShortHelp() []key.Binding {
	if k.step == stepReview {
		return []key.Binding{k.Create, k.Back, k.Quit}
	}
	return []key.Binding{k.Add, k.Edit, k.Next, k.Help}
}

// FullHelp is the help shown with ?.
func (k *wizardKeys) FullHelp() [][]key.Binding {
	if k.step == stepReview {
		return [][]key.Binding{k.ShortHelp()}
	}
	return [][]key.Binding{{k.Add, k.Edit, k.Delete}, {k.Up, k.Down}, {k.Next, k.Back, k.Quit}}
}

// WizardDefaults are the wizard's starting values (from the settings).
type WizardDefaults struct {
	Language, Visibility, Profile, Instructions string
	Duration                                    time.Duration
	ShuffleQuestions, ShuffleOptions            bool
}

// NewWizard starts the wizard on the details step.
func NewWizard(d WizardDefaults) *Wizard {
	or := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	dur := "45m"
	if d.Duration > 0 {
		dur = exam.FormatDuration(d.Duration)
	}
	m := &Wizard{lang: or(d.Language, exam.DefaultLanguage), duration: dur, visibility: or(d.Visibility, "score"),
		profile: or(d.Profile, "standard"), shuffleQ: d.ShuffleQuestions, shuffleO: d.ShuffleOptions,
		defaultInstr: d.Instructions, lens: true, frozenPointer: true, autoRead: true, readSpeed: "normal",
		width: 100, height: 40, help: help.New(),
		spin: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(accent)))}
	m.list = list.New(nil, questionDelegate{}, 40, 20)
	m.list.SetShowTitle(false)
	m.list.SetShowStatusBar(false)
	m.list.SetShowHelp(false)
	m.list.SetFilteringEnabled(false)
	m.list.DisableQuitKeybindings()
	m.list.Styles.NoItems = mutedStyle
	m.preview = viewport.New(viewport.WithWidth(50), viewport.WithHeight(20))
	m.layout()
	return m
}

// Init opens the first form.
func (m *Wizard) Init() tea.Cmd { return m.detailsForm() }

func (m *Wizard) dirty() bool { return m.title != "" || len(m.questions) > 0 }

// ---------------- update ----------------

// Update routes messages to the open form, the confirmation, or the step.
func (m *Wizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
	case spinner.TickMsg:
		if m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case checkedMsg:
		m.busy, m.checked = "", true
		m.problems = msg.probs
		if msg.err != nil {
			m.say(msg.err)
		}
		return m, nil
	case createdMsg:
		m.busy = ""
		if msg.err != nil {
			m.say(msg.err)
			return m, nil
		}
		m.Dir, m.step = m.folderPath(), stepDone
		return m, m.doneForm()
	}

	if m.form != nil {
		return m, m.updateForm(msg)
	}
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if m.confirm != "" {
		yes := m.onYes
		m.confirm, m.onYes = "", nil
		if isYes(k.String()) {
			return m, yes()
		}
		m.notice = ""
		return m, m.resume()
	}
	if k.String() == "ctrl+c" {
		return m, m.quit()
	}
	switch m.step {
	case stepQuestions:
		return m, m.questionsKey(k)
	case stepReview:
		return m, m.reviewKey(k)
	}
	return m, nil
}

func (m *Wizard) updateForm(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			m.form = nil
			return m.onEsc()
		case "ctrl+c":
			return m.quit()
		}
	}
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	switch m.form.State {
	case huh.StateCompleted:
		m.form = nil
		return m.onSubmit()
	case huh.StateAborted:
		m.form = nil
		return m.onEsc()
	}
	return cmd
}

// open shows a form; submit runs when it completes and esc when it is left.
func (m *Wizard) open(f *huh.Form, submit, esc func() tea.Cmd) tea.Cmd {
	m.form = StyleForm(f).WithShowHelp(true).WithWidth(min(m.contentWidth(), 90))
	m.onSubmit, m.onEsc = submit, esc
	return m.form.Init()
}

func (m *Wizard) quit() tea.Cmd {
	if !m.dirty() || m.step == stepDone {
		return tea.Quit
	}
	m.form = nil
	m.confirm = T("wizard.confirm_quit")
	m.onYes = func() tea.Cmd { return tea.Quit }
	return nil
}

// resume reopens the form of the current step after a cancelled question.
func (m *Wizard) resume() tea.Cmd {
	switch m.step {
	case stepDetails:
		return m.detailsForm()
	case stepProctoring:
		return m.proctoringForm()
	}
	return nil
}

func (m *Wizard) say(err error) { m.notice, m.isErr = err.Error(), true }

func (m *Wizard) tell(s string) { m.notice, m.isErr = s, false }

func (m *Wizard) goTo(step int) tea.Cmd {
	m.step, m.notice = step, ""
	switch step {
	case stepDetails:
		return m.detailsForm()
	case stepQuestions:
		m.refreshList()
		if len(m.questions) == 0 {
			return m.addQuestion()
		}
	case stepProctoring:
		return m.proctoringForm()
	case stepReview:
		m.checked, m.busy = false, T("wizard.checking")
		return tea.Batch(m.spin.Tick, m.check())
	}
	return nil
}

func (m *Wizard) questionsKey(k tea.KeyPressMsg) tea.Cmd {
	keys := newWizardKeys()
	i := m.list.Index()
	switch {
	case key.Matches(k, keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
	case key.Matches(k, keys.Add):
		return m.addQuestion()
	case key.Matches(k, keys.Edit):
		if i >= 0 && i < len(m.questions) {
			return m.editQuestion(i)
		}
	case key.Matches(k, keys.Delete):
		if i >= 0 && i < len(m.questions) {
			m.confirm = T("wizard.confirm_delete", "n", i+1)
			m.onYes = func() tea.Cmd {
				m.questions = slices.Delete(m.questions, i, i+1)
				m.refreshList()
				m.tell(T("wizard.deleted"))
				return nil
			}
		}
	case key.Matches(k, keys.Up), key.Matches(k, keys.Down):
		j := i + 1
		if key.Matches(k, keys.Up) {
			j = i - 1
		}
		if i >= 0 && j >= 0 && j < len(m.questions) {
			m.questions[i], m.questions[j] = m.questions[j], m.questions[i]
			m.refreshList()
			m.list.Select(j)
			m.refreshPreview()
		}
	case key.Matches(k, keys.Next):
		if len(m.questions) == 0 {
			m.say(errors.New(T("wizard.err.no_questions")))
			return nil
		}
		return m.goTo(stepProctoring)
	case key.Matches(k, keys.Back):
		return m.goTo(stepDetails)
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(k)
		m.refreshPreview()
		return cmd
	}
	return nil
}

func (m *Wizard) reviewKey(k tea.KeyPressMsg) tea.Cmd {
	if m.busy != "" {
		return nil
	}
	switch k.String() {
	case "enter":
		if !m.checked || m.problems.HasErrors() {
			m.say(errors.New(T("wizard.err.fix_first")))
			return nil
		}
		m.busy = T("wizard.creating")
		return tea.Batch(m.spin.Tick, m.create())
	case "1":
		return m.goTo(stepDetails)
	case "2":
		return m.goTo(stepQuestions)
	case "3":
		return m.goTo(stepProctoring)
	case "esc", "shift+tab", "left":
		return m.goTo(stepProctoring)
	}
	return nil
}

// ---------------- forms ----------------

var durationPresets = []string{"15m", "20m", "30m", "45m", "1h", "1h30m", "2h"}

func (m *Wizard) detailsForm() tea.Cmd {
	durOpts := []huh.Option[string]{}
	for _, d := range durationPresets {
		durOpts = append(durOpts, huh.NewOption(humanDuration(d), d))
	}
	durOpts = append(durOpts, huh.NewOption(T("wizard.other_duration"), "other"))
	if !slices.Contains(durationPresets, m.duration) && m.duration != "other" {
		m.otherDuration, m.duration = m.duration, "other"
	}
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title(T("wizard.intro.title")).Description(T("wizard.intro.body")),
			huh.NewInput().Title(T("form.manifest.exam_title")).Description(T("wizard.title_hint")).Placeholder(T("wizard.title_placeholder")).
				Value(&m.title).Validate(required(T("form.err.title"))),
			huh.NewInput().Title(T("wizard.folder")).Description(T("wizard.folder_hint")).PlaceholderFunc(func() string { return defaultFolder(m.title) }, &m.title).
				Value(&m.folder).Validate(func(s string) error { return checkFolder(m.pathFor(s)) }),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title(T("wizard.how_long")).Options(durOpts...).Value(&m.duration),
		),
		huh.NewGroup(
			huh.NewInput().Title(T("form.manifest.duration")).Description(T("wizard.duration_hint")).Value(&m.otherDuration).Validate(checkDuration),
		).WithHideFunc(func() bool { return m.duration != "other" }),
		huh.NewGroup(
			huh.NewSelect[string]().Title(T("form.manifest.language")).Options(languageOptions(m.lang)...).Value(&m.lang),
			huh.NewSelect[string]().Title(T("form.manifest.visibility")).Options(
				huh.NewOption(T("wizard.visibility.score"), "score"),
				huh.NewOption(T("wizard.visibility.review"), "review"),
				huh.NewOption(T("wizard.visibility.none"), "none")).Value(&m.visibility),
			huh.NewText().Title(T("wizard.instructions")).DescriptionFunc(func() string {
				return T("wizard.instructions_hint", "default", firstLine(m.defaultInstructions()))
			}, &m.lang).Lines(4).Value(&m.instructions),
		),
	)
	return m.open(f, func() tea.Cmd { return m.goTo(stepQuestions) }, m.quit)
}

func (m *Wizard) proctoringForm() tea.Cmd {
	f := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(T("wizard.how_proctored")).Options(
			huh.NewOption(T("wizard.profile.standard"), "standard"),
			huh.NewOption(T("wizard.profile.strict"), "strict"),
			huh.NewOption(T("wizard.profile.off"), "off")).Value(&m.profile),
		huh.NewNote().TitleFunc(func() string { return T("wizard.students_notice") }, &m.profile).
			DescriptionFunc(func() string { return T("wizard.profile_text." + m.profile) }, &m.profile),
		huh.NewConfirm().Title(T("form.manifest.shuffle_q")).Description(T("wizard.shuffle_q_hint")).
			Affirmative(T("common.yes")).Negative(T("common.no")).Value(&m.shuffleQ),
		huh.NewConfirm().Title(T("form.manifest.shuffle_o")).Description(T("wizard.shuffle_o_hint")).
			Affirmative(T("common.yes")).Negative(T("common.no")).Value(&m.shuffleO),
	), huh.NewGroup(
		huh.NewNote().Title(T("wizard.photos")).Description(T("wizard.photos_hint")),
		huh.NewConfirm().Title(T("form.manifest.lens")).Description(T("form.manifest.lens_hint")).
			Affirmative(T("common.yes")).Negative(T("common.no")).Value(&m.lens),
		huh.NewConfirm().Title(T("form.manifest.auto_read")).Description(T("form.manifest.auto_read_hint")).
			Affirmative(T("common.yes")).Negative(T("common.no")).Value(&m.autoRead),
		huh.NewSelect[string]().Title(T("form.manifest.read_speed")).Description(T("form.manifest.read_speed_hint")).
			Options(readSpeedOptions()...).Value(&m.readSpeed),
		huh.NewConfirm().Title(T("wizard.frozen")).Description(T("wizard.frozen_hint")).
			Affirmative(T("common.yes")).Negative(T("common.no")).Value(&m.frozenPointer),
	).WithHideFunc(func() bool { return m.profile == "off" }))
	return m.open(f, func() tea.Cmd { return m.goTo(stepReview) }, func() tea.Cmd { return m.goTo(stepQuestions) })
}

func (m *Wizard) addQuestion() tea.Cmd {
	v := newQuestionValues("")
	return m.open(v.form(nil), func() tea.Cmd {
		q, a, err := v.build()
		if err != nil {
			m.say(err)
			return nil
		}
		m.questions = append(m.questions, &wizQuestion{q: q, a: a})
		m.refreshList()
		m.list.Select(len(m.questions) - 1)
		m.refreshPreview()
		m.tell(T("wizard.added", "n", len(m.questions)))
		return nil
	}, func() tea.Cmd {
		m.step = stepQuestions
		m.tell(T("wizard.discarded"))
		return nil
	})
}

func (m *Wizard) editQuestion(i int) tea.Cmd {
	wq := m.questions[i]
	v := questionValuesOf(wq.q, wq.a)
	v.old = nil // nothing is on disk yet: the type can still change
	return m.open(v.form(nil), func() tea.Cmd {
		q, a, err := v.build()
		if err != nil {
			m.say(err)
			return nil
		}
		m.questions[i] = &wizQuestion{q: q, a: a}
		m.refreshList()
		m.refreshPreview()
		m.tell(T("wizard.saved", "n", i+1))
		return nil
	}, func() tea.Cmd {
		m.tell(T("editor.discarded"))
		return nil
	})
}

func (m *Wizard) doneForm() tea.Cmd {
	next := NextStart
	f := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("✓ "+T("wizard.done.title")).Description(T("wizard.done.body", "dir", m.Dir)),
		huh.NewSelect[string]().Title(T("wizard.done.next")).Options(
			huh.NewOption(T("wizard.done.start"), NextStart),
			huh.NewOption(T("wizard.done.edit"), NextEdit),
			huh.NewOption(T("home.quit"), "")).Value(&next),
	))
	return m.open(f, func() tea.Cmd {
		m.Next = next
		return tea.Quit
	}, func() tea.Cmd { return tea.Quit })
}

// ---------------- building ----------------

func (m *Wizard) pathFor(folder string) string {
	if f := strings.TrimSpace(folder); f != "" {
		return f
	}
	return defaultFolder(m.title)
}

func (m *Wizard) folderPath() string { return m.pathFor(m.folder) }

func (m *Wizard) durationValue() time.Duration {
	s := m.duration
	if s == "other" {
		s = m.otherDuration
	}
	d, _ := time.ParseDuration(strings.TrimSpace(s))
	return d
}

// build assembles the exam; question ids follow the order: q001, q002…
func (m *Wizard) build() *exam.Exam {
	id := exam.Slug(filepath.Base(filepath.Clean(m.folderPath())))
	instr := strings.TrimSpace(m.instructions)
	if instr == "" {
		instr = m.defaultInstructions()
	}
	sq, so := m.shuffleQ, m.shuffleO
	proc := exam.Proctoring{Profile: m.profile}
	if m.profile != "off" {
		// Written only when switched off: on is the default.
		off := func(on bool) *bool {
			if on {
				return nil
			}
			return &on
		}
		proc.Lens, proc.FrozenPointer = off(m.lens), off(m.frozenPointer)
		proc.AutoRead = off(m.autoRead)
		if m.readSpeed != "" && m.readSpeed != "normal" {
			proc.ReadSpeed = m.readSpeed
		}
	}
	ex := &exam.Exam{
		Manifest: exam.Manifest{Format: exam.FormatV1, ID: id, Title: strings.TrimSpace(m.title), Language: m.lang,
			Duration: exam.Duration(m.durationValue()), ResultVisibility: m.visibility, Instructions: instr,
			Order: exam.Order{ShuffleQuestions: &sq, ShuffleOptions: &so}, Proctoring: proc},
		Questions: map[string]*exam.Question{},
		Key:       exam.AnswerKey{Format: exam.FormatV1, Exam: id, Answers: map[string]exam.Answer{}},
	}
	for i, wq := range m.questions {
		q := *wq.q
		q.ID = fmt.Sprintf("q%03d", i+1)
		ex.Questions[q.ID] = &q
		ex.Key.Answers[q.ID] = wq.a
		ex.Manifest.Items = append(ex.Manifest.Items, exam.Item{Question: q.ID})
	}
	return ex
}

// check writes the exam to a scratch folder and validates it there.
func (m *Wizard) check() tea.Cmd {
	ex := m.build()
	return func() tea.Msg {
		tmp, err := os.MkdirTemp("", "ax-new-")
		if err != nil {
			return checkedMsg{err: err}
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		dir := filepath.Join(tmp, ex.Manifest.ID)
		if err := exam.Create(dir, ex); err != nil {
			return checkedMsg{err: err}
		}
		_, probs, err := exam.Load(dir)
		for i := range probs {
			probs[i].File = strings.TrimPrefix(probs[i].File, dir)
		}
		return checkedMsg{probs: probs, err: err}
	}
}

func (m *Wizard) create() tea.Cmd {
	ex, dir := m.build(), m.folderPath()
	return func() tea.Msg {
		if err := checkFolder(dir); err != nil {
			return createdMsg{err: err}
		}
		return createdMsg{err: exam.Create(dir, ex)}
	}
}

// ---------------- view ----------------

func (m *Wizard) contentWidth() int { return max(60, min(m.width, 130)) - 2 }

func (m *Wizard) layout() {
	w := m.contentWidth()
	h := max(8, m.height-9-lipgloss.Height(m.help.View(newWizardKeys())))
	lw := min(46, w*2/5)
	m.list.SetSize(lw, h)
	m.preview.SetWidth(w - lw - 3)
	m.preview.SetHeight(h)
	m.refreshPreview()
}

func (m *Wizard) refreshList() {
	items := make([]list.Item, len(m.questions))
	for i, q := range m.questions {
		items[i] = q
	}
	m.list.SetItems(items)
	m.refreshPreview()
}

func (m *Wizard) refreshPreview() {
	i := m.list.Index()
	if i < 0 || i >= len(m.questions) {
		m.preview.SetContent(mutedStyle.Render(T("wizard.empty")))
		return
	}
	m.preview.SetContent(previewQuestion(m.questions[i], i+1, m.preview.Width()))
	m.preview.GotoTop()
}

// View draws the stepper, the current step and the help.
func (m *Wizard) View() tea.View {
	var b strings.Builder
	b.WriteString(brandStyle.Render("ASSESS") + mutedStyle.Render("  ·  "+T("wizard.name")) + "\n")
	b.WriteString(m.stepper() + "\n" + mutedStyle.Render(strings.Repeat("─", m.contentWidth())) + "\n")

	switch {
	case m.form != nil:
		b.WriteString(m.form.View() + "\n")
	case m.step == stepQuestions:
		b.WriteString(m.questionsView())
	case m.step == stepReview:
		b.WriteString(m.reviewView())
	}

	b.WriteString("\n")
	switch {
	case m.confirm != "":
		yes, no := yesNoKeys()
		b.WriteString(accentStyle.Render(m.confirm) + "  " + keyStyle.Render(strings.ToUpper(yes[0])) + " " + T("common.yes_short") + "  " +
			keyStyle.Render(strings.ToUpper(no[0])) + " " + T("common.no_short") + "\n")
	case m.busy != "":
		b.WriteString(m.spin.View() + " " + m.busy + "\n")
	case m.notice != "" && m.isErr:
		b.WriteString(accentStyle.Render(m.notice) + "\n")
	case m.notice != "":
		b.WriteString(m.notice + "\n")
	}
	if m.form == nil && (m.step == stepQuestions || m.step == stepReview) {
		keys := newWizardKeys()
		keys.step = m.step
		b.WriteString(m.help.View(keys))
	} else if m.form != nil {
		b.WriteString(mutedStyle.Render(T("wizard.form_hint")))
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *Wizard) stepper() string {
	var parts []string
	for i, key := range stepKeys {
		n := T(key)
		label := fmt.Sprintf(" %d %s ", i+1, n)
		switch {
		case i == m.step || (m.step == stepDone && i == stepReview):
			parts = append(parts, selStyle.Render(label))
		case i < m.step:
			parts = append(parts, lipgloss.NewStyle().Foreground(accent).Render(" ✓ "+n+" "))
		default:
			parts = append(parts, mutedStyle.Render(label))
		}
	}
	return strings.Join(parts, mutedStyle.Render("─"))
}

func (m *Wizard) questionsView() string {
	total := 0.0
	for _, q := range m.questions {
		total += q.a.Points
	}
	head := headStyle.Render(T("wizard.n_questions", "n", len(m.questions)) + " · " + plural2(total))
	left := lipgloss.NewStyle().Width(m.list.Width()).Render(head + "\n\n" + m.list.View())
	right := lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).BorderForeground(muted).PaddingLeft(2).
		Render(headStyle.Render(T("wizard.preview_title")) + "\n\n" + m.preview.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right) + "\n"
}

func plural2(points float64) string {
	if points == 1 {
		return T("wizard.points", "n", 1, "v", "1")
	}
	return T("wizard.points", "n", 2, "v", num(points))
}

func (m *Wizard) reviewView() string {
	if m.busy != "" && !m.checked {
		return ""
	}
	ex := m.build()
	man := ex.Manifest
	types := map[string]int{}
	total := 0.0
	for _, q := range m.questions {
		types[q.q.Type]++
		total += q.a.Points
	}
	var kinds []string
	for _, t := range []string{exam.TypeSingle, exam.TypeMultiple, exam.TypeTrueFalse, exam.TypeShort} {
		if types[t] > 0 {
			kinds = append(kinds, fmt.Sprintf("%d %s", types[t], typeName(t)))
		}
	}
	vis := T("wizard.review.visibility." + man.ResultVisibility)
	shuffle := T("wizard.review.shuffle." + strconv.FormatBool(m.shuffleQ) + "_" + strconv.FormatBool(m.shuffleO))
	row := func(k, v string) string { return "  " + mutedStyle.Render(fmt.Sprintf("%-16s", k)) + v + "\n" }
	var b strings.Builder
	b.WriteString(boldStyle.Render("  "+man.Title) + "\n\n")
	b.WriteString(row(T("wizard.folder"), m.folderPath()))
	b.WriteString(row(T("form.manifest.duration"), humanDuration(exam.FormatDuration(man.Duration.D()))))
	b.WriteString(row(T("wizard.review.language"), languageName(man.Language)))
	b.WriteString(row(T("wizard.review.sees"), vis))
	b.WriteString(row(T("wizard.step.questions"), fmt.Sprintf("%d (%s) · %s", len(m.questions), strings.Join(kinds, ", "), plural2(total))))
	b.WriteString(row(T("form.manifest.proctoring"), T("profile."+m.profile)))
	if m.profile != "off" {
		var photo []string
		for _, p := range []struct {
			on   bool
			name string
		}{{m.lens, T("wizard.review.lens")}, {m.frozenPointer, T("wizard.review.frozen")}} {
			if p.on {
				photo = append(photo, p.name)
			}
		}
		if len(photo) == 0 {
			photo = []string{T("wizard.review.none")}
		}
		b.WriteString(row(T("wizard.review.photos"), strings.Join(photo, ", ")))
	}
	b.WriteString(row(T("wizard.review.shuffles"), shuffle))
	b.WriteString("\n")
	switch {
	case !m.checked:
	case len(m.problems) == 0:
		b.WriteString(lipgloss.NewStyle().Foreground(accent).Bold(true).Render("  ✓ "+T("wizard.review.ok")) + " " + T("wizard.review.ok_hint") + "\n")
	default:
		if m.problems.HasErrors() {
			b.WriteString(accentStyle.Render("  ✕ "+T("wizard.review.errors")) + "\n")
		} else {
			b.WriteString(boldStyle.Render("  ⚠ "+T("wizard.review.warnings")) + "\n")
		}
		for _, p := range m.problems {
			b.WriteString("    " + cut(p.String(), m.contentWidth()-4) + "\n")
		}
	}
	return b.String()
}

// questionDelegate draws a question as two lines: number, type and points,
// then the start of the prompt.
type questionDelegate struct{}

func (questionDelegate) Height() int                         { return 2 }
func (questionDelegate) Spacing() int                        { return 1 }
func (questionDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (questionDelegate) Render(w io.Writer, m list.Model, i int, item list.Item) {
	q := item.(*wizQuestion)
	head := fmt.Sprintf("%d. %s · %s", i+1, typeName(q.q.Type), plural2(q.a.Points))
	if q.q.TimeLimit != 0 {
		head += " · " + exam.FormatDuration(q.q.TimeLimit.D())
	}
	text := cut(plainPrompt(q.q.Prompt), max(10, m.Width()-4))
	if i == m.Index() {
		fmt.Fprint(w, accentStyle.Render("▌ "+head)+"\n"+accentStyle.Render("▌ ")+text)
		return
	}
	fmt.Fprint(w, "  "+boldStyle.Render(head)+"\n  "+mutedStyle.Render(text))
}

// previewQuestion shows a question roughly as the student sees it, with the
// right answer marked for the teacher.
func previewQuestion(wq *wizQuestion, n, width int) string {
	q, a := wq.q, wq.a
	w := max(20, width-2)
	ok := lipgloss.NewStyle().Foreground(accent).Bold(true)
	var b strings.Builder
	meta := T("wizard.preview.meta", "n", n, "type", strings.ToUpper(typeName(q.Type)), "points", plural2(a.Points))
	if q.TimeLimit != 0 {
		meta += " · " + T("wizard.preview.limit", "d", exam.FormatDuration(q.TimeLimit.D()))
	}
	b.WriteString(mutedStyle.Render(meta) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Width(w).Render(strings.TrimSpace(q.Prompt)) + "\n\n")
	optLine := func(mark, text string, right bool) string {
		line := text
		if lipgloss.Width(text) > w-8 {
			line = strings.ReplaceAll(lipgloss.NewStyle().Width(w-8).Render(text), "\n", "\n     ")
		}
		if right {
			return ok.Render(mark) + "  " + ok.Render(line) + ok.Render("  ✓") + "\n"
		}
		return mutedStyle.Render(mark) + "  " + line + "\n"
	}
	switch q.Type {
	case exam.TypeSingle:
		for _, o := range q.Options {
			mark := "( )"
			if o.ID == a.Option {
				mark = "(●)"
			}
			b.WriteString(optLine(mark, o.Text, o.ID == a.Option))
		}
	case exam.TypeMultiple:
		for _, o := range q.Options {
			right := slices.Contains(a.Options, o.ID)
			mark := "[ ]"
			if right {
				mark = "[■]"
			}
			b.WriteString(optLine(mark, o.Text, right))
		}
		if a.Partial {
			b.WriteString("\n" + mutedStyle.Render(T("wizard.preview.partial")))
		} else {
			b.WriteString("\n" + mutedStyle.Render(T("wizard.preview.all_or_nothing")))
		}
	case exam.TypeTrueFalse:
		v := a.Value != nil && *a.Value
		b.WriteString(optLine("( )", T("wizard.preview.true"), v))
		b.WriteString(optLine("( )", T("wizard.preview.false"), !v))
	case exam.TypeShort:
		box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Width(min(w, 40)).Foreground(muted)
		if q.Input == "number" {
			b.WriteString(box.Render(T("wizard.preview.write_number")) + "\n")
			if a.Number != nil {
				ans := T("wizard.preview.right", "v", num(*a.Number))
				if a.Tolerance != nil && *a.Tolerance > 0 {
					ans += " (± " + num(*a.Tolerance) + ")"
				}
				b.WriteString(ok.Render(ans))
			}
		} else {
			b.WriteString(box.Render(T("wizard.preview.write_text")) + "\n")
			b.WriteString(ok.Render(T("wizard.preview.accepted", "list", strings.Join(a.Accept, " · "))))
			b.WriteString("\n" + mutedStyle.Render(T("wizard.preview.lenient")))
		}
	}
	return b.String()
}

// ---------------- helpers ----------------

func required(msg string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(msg)
		}
		return nil
	}
}

func checkDuration(s string) error {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < time.Minute || d > exam.MaxDuration {
		return errors.New(T("form.err.duration"))
	}
	return nil
}

// checkFolder accepts a folder that does not exist yet or is empty, and
// whose name makes a valid exam id.
func checkFolder(dir string) error {
	if dir == "" {
		return errors.New(T("wizard.err.no_folder"))
	}
	if !exam.ValidID(exam.Slug(filepath.Base(filepath.Clean(dir)))) {
		return errors.New(T("wizard.err.folder_name"))
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return errors.New(T("wizard.err.folder_taken"))
	}
	return nil
}

// defaultFolder is ./<slug of the title>.
func defaultFolder(title string) string {
	if s := exam.Slug(title); s != "" {
		return "." + string(filepath.Separator) + s
	}
	return ""
}

// defaultInstructions are the settings' instructions for new exams, or a
// standard text in the students' language.
func (m *Wizard) defaultInstructions() string {
	if s := strings.TrimSpace(m.defaultInstr); s != "" {
		return s
	}
	return i18n.In(m.lang, "wizard.default_instructions")
}

// languageName is a language's own name, or its code.
func languageName(lang string) string {
	for _, c := range i18n.Languages() {
		if c.Lang == lang && c.Name != "" {
			return c.Name
		}
	}
	return lang
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, ".")
	return s
}

func humanDuration(s string) string {
	d, err := time.ParseDuration(s)
	if err != nil {
		return s
	}
	h, mi := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h == 0:
		return T("wizard.dur.minutes", "n", mi)
	case mi == 0:
		return T("wizard.dur.hours", "n", h)
	}
	return T("wizard.dur.hours_minutes", "h", T("wizard.dur.hours", "n", h), "m", T("wizard.dur.minutes", "n", mi))
}
