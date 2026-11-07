package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
)

// Steps of the exam screen.
const (
	stepDetails = iota
	stepQuestions
	stepRules
	stepReview
	stepCount
)

var stepKeys = []string{"wizard.step.details", "wizard.step.questions", "wizard.step.proctoring", "wizard.step.review"}

// Details fields, in screen order.
const (
	detTitle = iota
	detDuration
	detVisibility
	detLanguage
	detInstructions
	detCount
)

// Rules rows after the three proctoring profiles.
const (
	ruleShuffleQ = iota + 3
	ruleShuffleO
	ruleStrikes
	ruleLens
	ruleAutoRead
	ruleReadSpeed
	ruleFrozen
)

var (
	durationPresets = []time.Duration{30 * time.Minute, 45 * time.Minute, time.Hour, 90 * time.Minute}
	visibilities    = []string{"none", "score", "review"}
	profiles        = []string{"standard", "strict", "off"}
	readSpeeds      = []string{"slow", "normal", "fast"}
)

// Editor is the one screen for creating and editing an exam: a new exam is an
// empty one. Everything saves itself and an incomplete question is allowed;
// only starting the exam needs it finished.
type Editor struct {
	d        *exam.Draft // nil until a new exam has its title
	defaults ExamDefaults
	base     string // where a new exam's folder goes; empty is here
	man      *manifestValues

	title, other, instr textField
	durChoice           int // index in durationPresets; len means another

	step, field int
	sel         int // the question rail, or the problem in Review
	scroll      int // reading a long question
	q           *questionEdit
	pasting     *textField // Pegar: many questions at once

	width, height int
	notice        string
	isErr         bool
	savedAt       time.Time
	confirm       string
	onYes         func() error
	showHelp      bool

	editor string // the editor setting; empty uses $VISUAL / $EDITOR

	// Dir is the exam's folder, empty if a new exam was never written.
	Dir string
	// Start reports that the editor closed asking to start the exam.
	Start bool
}

type editorRow struct {
	kind  int // rowQuestion, rowPool
	id    string
	item  int // index in the manifest items; -1 for loose questions
	inner bool
}

const (
	rowQuestion = iota
	rowPool
)

type editedMsg struct{ err error }

// ExamDefaults are a new exam's starting values (from the settings).
type ExamDefaults struct {
	Language, Visibility, Profile, Instructions string
	Duration                                    time.Duration
	ShuffleQuestions, ShuffleOptions            bool
}

// NewEditor opens the screen on an exam folder. editor is the command that
// opens files (the editor setting); empty uses $VISUAL or $EDITOR.
func NewEditor(d *exam.Draft, editor string) *Editor {
	m := &Editor{d: d, man: manifestValuesOf(d.Exam.Manifest), step: stepQuestions, width: 120, height: 36, editor: editor, Dir: d.Dir}
	m.loadFields()
	return m
}

// NewExam opens the screen on an empty exam; nothing is written until it has a title.
func NewExam(def ExamDefaults, editor string) *Editor {
	or := func(v, d string) string {
		if v == "" {
			return d
		}
		return v
	}
	lang := or(def.Language, exam.DefaultLanguage)
	dur := def.Duration
	if dur <= 0 {
		dur = 45 * time.Minute
	}
	sq, so := def.ShuffleQuestions, def.ShuffleOptions
	man := exam.Manifest{Format: exam.FormatV1, Language: lang, Duration: exam.Duration(dur), ResultVisibility: or(def.Visibility, "score"),
		Order: exam.Order{ShuffleQuestions: &sq, ShuffleOptions: &so}, Proctoring: exam.Proctoring{Profile: or(def.Profile, "standard")}}
	m := &Editor{defaults: def, man: manifestValuesOf(man), width: 120, height: 36, editor: editor}
	m.man.Instructions = m.defaultInstructions(lang)
	m.loadFields()
	return m
}

func (m *Editor) loadFields() {
	m.title, m.instr = newField(m.man.Title), newField(m.man.Instructions)
	d, _ := time.ParseDuration(m.man.Duration)
	m.durChoice = slices.Index(durationPresets, d)
	if m.durChoice < 0 {
		m.durChoice = len(durationPresets)
		m.other = newField(m.man.Duration)
	}
}

// defaultInstructions are the settings' text for new exams, or a standard
// text in the students' language.
func (m *Editor) defaultInstructions(lang string) string {
	if s := strings.TrimSpace(m.defaults.Instructions); s != "" {
		return s
	}
	return i18n.In(lang, "wizard.default_instructions")
}

// Init asks for the terminal's background, which picks the theme.
func (m *Editor) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m *Editor) rows() []editorRow {
	if m.d == nil {
		return nil
	}
	var rows []editorRow
	seen := map[string]bool{}
	for i, it := range m.d.Exam.Manifest.Items {
		if it.Pool != nil {
			rows = append(rows, editorRow{kind: rowPool, id: it.Pool.ID, item: i})
			for _, id := range it.Pool.From {
				seen[id] = true
				rows = append(rows, editorRow{kind: rowQuestion, id: id, item: i, inner: true})
			}
			continue
		}
		seen[it.Question] = true
		rows = append(rows, editorRow{kind: rowQuestion, id: it.Question, item: i})
	}
	for _, id := range m.d.QuestionOrder() {
		if !seen[id] {
			rows = append(rows, editorRow{kind: rowQuestion, id: id, item: -1})
		}
	}
	return rows
}

func (m *Editor) selected() (editorRow, bool) {
	rows := m.rows()
	if len(rows) == 0 {
		return editorRow{}, false
	}
	m.sel = min(max(0, m.sel), len(rows)-1)
	return rows[m.sel], true
}

func (m *Editor) selectQuestion(id string) {
	for i, r := range m.rows() {
		if r.kind == rowQuestion && r.id == id {
			m.sel = i
		}
	}
}

func (m *Editor) fail(err error) {
	m.notice, m.isErr = err.Error(), true
	if errors.Is(err, exam.ErrBrokenFile) {
		m.notice += " " + T("editor.open_hint")
	}
}

func (m *Editor) tell(s string) { m.notice, m.isErr = s, false }

func (m *Editor) markSaved() {
	m.savedAt = time.Now()
	m.notice, m.isErr = "", false
}

// ---------------- saving ----------------

// persist writes the details and the rules. A new exam gets its folder here,
// the first time it has a title. It reports false when a value cannot be saved.
func (m *Editor) persist() bool {
	m.man.Title = m.title.value()
	m.man.Instructions = m.instr.value()
	if m.durChoice < len(durationPresets) {
		m.man.Duration = exam.FormatDuration(durationPresets[m.durChoice])
	} else {
		m.man.Duration = m.typedDuration()
	}
	if m.d == nil {
		if strings.TrimSpace(m.man.Title) == "" {
			return true
		}
		if err := m.create(); err != nil {
			m.fail(err)
			return false
		}
	}
	m.man.m = m.d.Exam.Manifest // the items change as questions do
	man, err := m.man.build()
	if err == nil {
		err = m.d.SaveManifest(man)
	}
	if err != nil {
		m.fail(err)
		return false
	}
	m.markSaved()
	return true
}

// create writes an empty exam in a new folder named after the title.
func (m *Editor) create() error {
	slug := exam.Slug(m.man.Title)
	if slug == "" {
		slug = "exam"
	}
	for n := 1; n < 1000; n++ {
		name := slug
		if n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		dir := filepath.Join(m.base, defaultFolder(name))
		if checkFolder(dir) != nil {
			continue
		}
		man, err := m.man.build()
		if err != nil {
			return err
		}
		man.ID = exam.Slug(filepath.Base(filepath.Clean(dir)))
		ex := &exam.Exam{Manifest: man, Questions: map[string]*exam.Question{},
			Key: exam.AnswerKey{Format: exam.FormatV1, Exam: man.ID, Answers: map[string]exam.Answer{}}}
		if err := exam.Create(dir, ex); err != nil {
			return err
		}
		d, err := exam.OpenDraft(dir)
		if err != nil {
			return err
		}
		m.d, m.Dir = d, dir
		return nil
	}
	return errors.New(T("wizard.err.folder_name"))
}

// finish saves what is on screen and closes.
func (m *Editor) finish() tea.Cmd {
	if m.q != nil && !m.saveQuestion() {
		return nil
	}
	if (m.step == stepDetails || m.step == stepRules) && !m.persist() {
		return nil
	}
	return tea.Quit
}

func (m *Editor) goTo(step int) {
	step = min(max(0, step), stepCount-1)
	if (m.step == stepDetails || m.step == stepRules) && !m.persist() {
		return
	}
	if m.d == nil {
		m.tell(T("editor.ui.title_first"))
		return
	}
	m.step, m.field, m.notice, m.scroll = step, 0, "", 0
	if step == stepReview {
		m.sel = 0
	}
}

// ---------------- update ----------------

// Update routes messages to the current step.
func (m *Editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case editedMsg:
		err := msg.err
		if err == nil {
			err = m.d.Reload()
		}
		if err != nil {
			m.fail(err)
		} else {
			m.man = manifestValuesOf(m.d.Exam.Manifest)
			m.loadFields()
			m.tell(T("editor.edited"))
		}
	case tea.PasteMsg:
		m.paste(msg.Content)
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Editor) paste(s string) {
	switch {
	case m.pasting != nil:
		m.pasting.insert(s)
	case m.q != nil:
		m.q.paste(s)
	case m.step == stepDetails && m.field == detTitle:
		m.title.insert(strings.ReplaceAll(s, "\n", " "))
	case m.step == stepDetails && m.field == detInstructions:
		m.instr.insert(s)
	}
}

func (m *Editor) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if m.confirm != "" {
		yes := m.onYes
		m.confirm, m.onYes = "", nil
		m.notice = ""
		if isYes(s) {
			if err := yes(); err != nil {
				m.fail(err)
			} else {
				m.markSaved()
			}
		}
		return nil
	}
	if m.showHelp {
		if s == "?" || s == "esc" {
			m.showHelp = false
		}
		return nil
	}
	if s == "ctrl+c" {
		return m.finish()
	}
	if m.pasting != nil {
		m.pasteKey(k)
		return nil
	}
	if m.q != nil {
		return m.writingKey(k)
	}
	switch s {
	case "tab":
		m.goTo(m.step + 1)
		return nil
	case "shift+tab":
		m.goTo(m.step - 1)
		return nil
	case "esc":
		return m.finish()
	}
	switch m.step {
	case stepDetails:
		m.detailsKey(k)
	case stepRules:
		if s == "q" || s == "Q" {
			return m.finish()
		}
		if s == "?" {
			m.showHelp = true
		}
		m.rulesKey(s)
	case stepQuestions:
		return m.questionsKey(s)
	case stepReview:
		return m.reviewKey(s)
	}
	return nil
}

func (m *Editor) detailsKey(k tea.KeyPressMsg) {
	s := k.String()
	move := func(d int) { m.field = min(max(0, m.field+d), detCount-1) }
	switch m.field {
	case detTitle:
		if s == "enter" {
			move(1)
			return
		}
		if m.title.key(k, false) {
			return
		}
	case detInstructions:
		if m.instr.key(k, true) {
			return
		}
	case detDuration:
		if m.durChoice == len(durationPresets) && s != "left" && s != "right" && s != "up" && s != "down" && m.other.key(k, false) {
			return
		}
	}
	switch s {
	case "up":
		move(-1)
	case "down", "enter":
		move(1)
	case "left":
		m.turn(-1)
	case "right":
		m.turn(1)
	}
}

// turn moves the choice of the focused chip row.
func (m *Editor) turn(d int) {
	step := func(cur, n int) int { return (cur + d + n) % n }
	switch m.field {
	case detDuration:
		m.durChoice = step(m.durChoice, len(durationPresets)+1)
		if m.durChoice == len(durationPresets) {
			m.other = textField{} // the teacher types here; until then the last duration stands
		}
	case detVisibility:
		m.man.Visibility = visibilities[step(max(0, slices.Index(visibilities, m.man.Visibility)), len(visibilities))]
	case detLanguage:
		langs := m.languages()
		next := langs[step(max(0, slices.Index(langs, m.man.Language)), len(langs))]
		if m.d == nil && strings.TrimSpace(m.instr.value()) == strings.TrimSpace(m.defaultInstructions(m.man.Language)) {
			m.instr = newField(m.defaultInstructions(next))
		}
		m.man.Language = next
	default:
		return
	}
	m.persist()
}

// typedDuration reads the free duration: a bare number is minutes, and an
// empty field keeps the last duration.
func (m *Editor) typedDuration() string {
	s := strings.TrimSpace(m.other.value())
	switch {
	case s == "":
		return m.man.Duration
	case strings.Trim(s, "0123456789") == "":
		return s + "m"
	}
	return s
}

func (m *Editor) languages() []string {
	var out []string
	for _, c := range i18n.Languages() {
		out = append(out, c.Lang)
	}
	if !slices.Contains(out, m.man.Language) {
		out = append(out, m.man.Language)
	}
	return out
}

func (m *Editor) ruleRows() []int {
	rows := []int{0, 1, 2, ruleShuffleQ, ruleShuffleO}
	if m.man.Profile != "off" {
		rows = append(rows, ruleStrikes, ruleLens, ruleAutoRead, ruleReadSpeed, ruleFrozen)
	}
	return rows
}

func (m *Editor) rulesKey(s string) {
	rows := m.ruleRows()
	at := max(0, slices.Index(rows, m.field))
	switch s {
	case "up":
		m.field = rows[max(0, at-1)]
	case "down":
		m.field = rows[min(len(rows)-1, at+1)]
	case "space", "enter", " ":
		switch m.field {
		case 0, 1, 2:
			m.man.Profile = profiles[m.field]
		case ruleShuffleQ:
			m.man.ShuffleQ = !m.man.ShuffleQ
		case ruleShuffleO:
			m.man.ShuffleO = !m.man.ShuffleO
		case ruleLens:
			m.man.Lens = !m.man.Lens
		case ruleAutoRead:
			m.man.AutoRead = !m.man.AutoRead
		case ruleFrozen:
			m.man.FrozenPointer = !m.man.FrozenPointer
		default:
			return
		}
		m.persist()
	case "left", "right":
		d := 1
		if s == "left" {
			d = -1
		}
		switch m.field {
		case ruleStrikes: // 0 is the profile's own limit
			n, _ := strconv.Atoi(m.man.StrikeLimit)
			n = (n + d + 11) % 11
			m.man.StrikeLimit = ""
			if n > 0 {
				m.man.StrikeLimit = strconv.Itoa(n)
			}
		case ruleReadSpeed:
			at := max(0, slices.Index(readSpeeds, m.man.ReadSpeed))
			m.man.ReadSpeed = readSpeeds[(at+d+len(readSpeeds))%len(readSpeeds)]
		default:
			return
		}
		m.persist()
	}
}

func (m *Editor) questionsKey(s string) tea.Cmd {
	row, ok := m.selected()
	switch s {
	case "q", "Q":
		return m.finish()
	case "?":
		m.showHelp = true
	case "up", "k":
		m.sel, m.scroll = m.sel-1, 0
	case "down", "j":
		m.sel, m.scroll = m.sel+1, 0
	case "home", "g":
		m.sel, m.scroll = 0, 0
	case "end", "G":
		m.sel, m.scroll = len(m.rows())-1, 0
	case "pgdown":
		m.scroll += 5
	case "pgup":
		m.scroll = max(0, m.scroll-5)
	case "n", "N":
		m.q = newQuestionEdit(newQuestionValues(m.d.NextQuestionID()), true)
		m.notice = ""
	case "v", "V":
		m.pasting = &textField{}
		m.notice = ""
	case "enter":
		if !ok {
			return nil
		}
		if row.kind == rowPool {
			return m.openFile(exam.ManifestFile)
		}
		if q := m.d.Exam.Questions[row.id]; q != nil {
			m.q = newQuestionEdit(questionValuesOf(q, m.d.Exam.Key.Answers[row.id]), false)
			m.notice = ""
		}
	case "d", "D", "delete":
		if ok && row.kind == rowQuestion {
			id := row.id
			m.confirm = T("editor.confirm_delete", "id", id)
			m.onYes = func() error { return m.d.DeleteQuestion(id) }
		}
	case "shift+up", "K":
		m.move(row, ok, -1)
	case "shift+down", "J":
		m.move(row, ok, 1)
	case "e", "E":
		if ok {
			return m.external(row)
		}
	case "c", "C":
		return m.openFile(exam.AnswerKeyFile)
	case "r", "R":
		if err := m.d.Reload(); err != nil {
			m.fail(err)
		} else {
			m.tell(T("editor.reloaded"))
		}
	case "s", "S":
		if m.d.Problems.HasErrors() {
			m.fail(errors.New(T("editor.fix_first")))
			return nil
		}
		m.Start = true
		return tea.Quit
	}
	return nil
}

func (m *Editor) move(row editorRow, ok bool, delta int) {
	if !ok || row.item < 0 {
		return
	}
	if err := m.d.MoveItem(row.item, delta); err != nil {
		m.fail(err)
		return
	}
	for i, r := range m.rows() { // follow the moved item
		if r.item == row.item+delta && !r.inner {
			m.sel = i
			break
		}
	}
	m.markSaved()
}

func (m *Editor) reviewKey(s string) tea.Cmd {
	ps := m.d.Problems
	switch s {
	case "q", "Q":
		return m.finish()
	case "?":
		m.showHelp = true
	case "up":
		m.sel = max(0, m.sel-1)
	case "down":
		m.sel = min(max(0, len(ps)-1), m.sel+1)
	case "enter":
		if !ps.HasErrors() {
			m.Start = true
			return tea.Quit
		}
		m.jumpTo(ps[min(m.sel, len(ps)-1)])
	}
	return nil
}

// jumpTo opens the step that can fix a problem.
func (m *Editor) jumpTo(p exam.Problem) {
	if p.File == exam.ManifestFile {
		m.goTo(stepDetails)
		return
	}
	id := m.problemQuestion(p)
	m.goTo(stepQuestions)
	if id != "" {
		m.selectQuestion(id)
	}
}

// problemQuestion names the question a problem is about, if any.
func (m *Editor) problemQuestion(p exam.Problem) string {
	for id, q := range m.d.Exam.Questions {
		if p.File == q.File || p.File == exam.QuestionsDir+"/"+id+".yaml" ||
			p.Field == "answers."+id || strings.HasPrefix(p.Field, "answers."+id+".") ||
			(p.File == exam.AnswerKeyFile && strings.Contains(p.Message, strconv.Quote(id))) {
			return id
		}
	}
	return ""
}

// incomplete reports whether a question still has an error.
func (m *Editor) incomplete(id string) bool {
	for _, p := range m.d.Problems {
		if p.Severity == exam.SeverityError && m.problemQuestion(p) == id {
			return true
		}
	}
	return false
}

// external opens the file behind row in the user's editor.
func (m *Editor) external(row editorRow) tea.Cmd {
	if row.kind == rowQuestion {
		if q := m.d.Exam.Questions[row.id]; q != nil && q.File != "" {
			return m.openFile(q.File)
		}
	}
	return m.openFile(exam.ManifestFile)
}

func (m *Editor) openFile(rel string) tea.Cmd {
	c := EditorCommand(m.editor, filepath.Join(m.d.Dir, filepath.FromSlash(rel)))
	return tea.ExecProcess(c, func(err error) tea.Msg { return editedMsg{err} })
}

// EditorCommand opens path in the editor: the given command (the editor
// setting), $VISUAL or $EDITOR (which may carry arguments, such as
// "code -w"), or a sensible default for the system.
func EditorCommand(editor, path string) *exec.Cmd {
	ed := editor
	if strings.TrimSpace(ed) == "" {
		ed = os.Getenv("VISUAL")
	}
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(ed) == "" {
		ed = "vi"
		if runtime.GOOS == "windows" {
			ed = "notepad"
		}
	}
	parts := strings.Fields(ed)
	return exec.Command(parts[0], append(parts[1:], path)...) //nolint:gosec // G204: the user's own editor
}

// plainPrompt is the first meaningful line of a Markdown prompt.
func plainPrompt(md string) string {
	for l := range strings.SplitSeq(md, "\n") {
		l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "#>*_`-"))
		if l != "" && !strings.HasPrefix(l, "![") && !strings.HasPrefix(l, "|") {
			return strings.NewReplacer("**", "", "__", "", "`", "").Replace(l)
		}
	}
	return ""
}

// checkFolder accepts a folder that does not exist yet or is empty, and
// whose name makes a valid exam id.
func checkFolder(dir string) error {
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

// languageName is a language's own name, or its code.
func languageName(lang string) string {
	for _, c := range i18n.Languages() {
		if c.Lang == lang && c.Name != "" {
			return c.Name
		}
	}
	return lang
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
