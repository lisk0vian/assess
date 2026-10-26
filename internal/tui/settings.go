package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/i18n"
)

// SettingChange is one setting changed on the Settings screen.
type SettingChange struct {
	Key, Value string
	Reset      bool // back to the default (the line is commented out again)
}

// Settings edits the settings file: every key, grouped as in the file,
// with a form suited to its kind. Nothing is written here; the caller saves
// Changes when the screen closes.
type Settings struct {
	cfg           *config.Config
	sel           int
	width, height int
	pending       map[string]SettingChange
	order         []string // keys in the order they were changed

	form    *huh.Form
	editing string
	value   string
	choice  string

	asking bool // "save the changes?" on the way out
	notice string

	// Changes are the confirmed changes, in order, once the screen closes.
	Changes []SettingChange
}

// NewSettings opens the Settings screen on the loaded settings.
func NewSettings(cfg *config.Config) *Settings {
	return &Settings{cfg: cfg, width: 100, height: 40, pending: map[string]SettingChange{}}
}

// Init does nothing; the settings are already loaded.
func (m *Settings) Init() tea.Cmd { return nil }

// current is the value shown for k: pending, or the effective one.
func (m *Settings) current(k config.Key) (string, string) {
	if ch, ok := m.pending[k.Name]; ok {
		if ch.Reset {
			return k.Default, T("settings.will_reset")
		}
		return ch.Value, T("settings.changed")
	}
	v := m.cfg.Get(k.Name)
	switch v.Source {
	case config.FromFile:
		return v.Raw, T("cli.config.from_file", "line", v.Line)
	}
	return v.Raw, T("cli.config.from_default")
}

func (m *Settings) change(ch SettingChange) {
	if _, ok := m.pending[ch.Key]; !ok {
		m.order = append(m.order, ch.Key)
	}
	m.pending[ch.Key] = ch
}

// Update handles the list keys, the open form and the closing question.
func (m *Settings) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = ws.Width, ws.Height
	}
	if m.form != nil {
		return m, m.updateForm(msg)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	k := key.String()
	if m.asking {
		switch {
		case isYes(k):
			m.save()
			return m, tea.Quit
		case k == "esc":
			m.asking = false
			return m, nil
		}
		return m, tea.Quit // no: leave without saving
	}
	keys := config.Keys
	switch k {
	case "q", "Q", "esc", "ctrl+c":
		if len(m.pending) == 0 {
			return m, tea.Quit
		}
		m.asking = true
	case "up", "k":
		m.sel = max(0, m.sel-1)
	case "down", "j":
		m.sel = min(len(keys)-1, m.sel+1)
	case "home", "g":
		m.sel = 0
	case "end", "G":
		m.sel = len(keys) - 1
	case "enter":
		return m, m.edit(keys[m.sel])
	case "r", "R":
		kk := keys[m.sel]
		m.change(SettingChange{Key: kk.Name, Reset: true})
		m.notice = T("settings.reset_notice", "key", kk.Name, "value", display(kk.Default))
	case "s", "S":
		m.save()
		return m, tea.Quit
	}
	return m, nil
}

func (m *Settings) save() {
	m.Changes = nil
	for _, k := range m.order {
		m.Changes = append(m.Changes, m.pending[k])
	}
}

func (m *Settings) updateForm(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "esc" {
		m.form, m.notice = nil, T("editor.discarded")
		return nil
	}
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	switch m.form.State {
	case huh.StateCompleted:
		k, _ := config.Lookup(m.editing)
		v := m.value
		if k.Kind == config.Choice || k.Kind == config.Bool || isLangKey(k) {
			v = m.choice
		}
		v = strings.TrimSpace(v)
		m.form = nil
		if old, _ := m.current(k); old == v {
			m.notice = ""
			return nil
		}
		m.change(SettingChange{Key: k.Name, Value: v})
		m.notice = T("settings.changed_notice", "key", k.Name, "value", display(v))
		return nil
	case huh.StateAborted:
		m.form, m.notice = nil, T("editor.discarded")
	}
	return cmd
}

func isLangKey(k config.Key) bool { return k.Name == "language" || k.Name == "student_language" }

// edit opens the form for one key.
func (m *Settings) edit(k config.Key) tea.Cmd {
	cur, _ := m.current(k)
	m.editing, m.value, m.choice = k.Name, cur, cur
	title := k.Name
	desc := config.Help(i18n.Language(), k)
	var field huh.Field
	switch {
	case isLangKey(k):
		opts := languageOptions(cur)
		if k.Name == "student_language" {
			opts = append([]huh.Option[string]{huh.NewOption(T("settings.same_as_terminal"), "")}, opts...)
		}
		field = huh.NewSelect[string]().Title(title).Description(desc).Options(opts...).Value(&m.choice)
	case k.Kind == config.Choice:
		var opts []huh.Option[string]
		for _, c := range k.Choices {
			opts = append(opts, huh.NewOption(choiceLabel(k, c), c))
		}
		field = huh.NewSelect[string]().Title(title).Description(desc).Options(opts...).Value(&m.choice)
	case k.Kind == config.Bool:
		field = huh.NewSelect[string]().Title(title).Description(desc).Options(
			huh.NewOption(T("common.yes"), "true"), huh.NewOption(T("common.no"), "false")).Value(&m.choice)
	default:
		in := huh.NewInput().Title(title).Description(desc).Value(&m.value).Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" && k.Kind == config.String {
				return nil
			}
			return k.Validate(s)
		})
		if k.Default != "" {
			in = in.Placeholder(k.Default)
		}
		field = in
	}
	m.form = StyleForm(huh.NewForm(huh.NewGroup(field))).WithShowHelp(true).WithWidth(min(m.w()-4, 100))
	m.notice = ""
	return m.form.Init()
}

// choiceLabel explains a choice when the catalog has words for it.
func choiceLabel(k config.Key, c string) string {
	switch k.Name {
	case "new_exam.result_visibility":
		return T("form.visibility." + c)
	case "new_exam.proctoring":
		return T("form.profile." + c)
	}
	return c
}

func display(v string) string {
	if v == "" {
		return T("cli.config.empty")
	}
	return v
}

func (m *Settings) w() int { return max(72, min(m.width, 140)) }

// View draws the settings grouped as in the file, and the help of the
// selected one.
func (m *Settings) View() tea.View {
	w := m.w()
	var b strings.Builder
	b.WriteString(brandStyle.Render("ASSESS") + mutedStyle.Render("  ·  "+T("settings.name")+"  ·  "+m.cfg.Path) + "\n")
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)) + "\n")
	if m.form != nil {
		b.WriteString(m.form.View() + "\n\n" + mutedStyle.Render(T("editor.esc_discards")) + "\n")
		return m.finish(&b)
	}

	type line struct {
		text string
		key  int // index in config.Keys, -1 for a heading
	}
	var lines []line
	group := "-"
	for i, k := range config.Keys {
		if g := k.Group(); g != group {
			group = g
			title := T("settings.general")
			if g != "" {
				title = T("config.group." + g)
			}
			if len(lines) > 0 {
				lines = append(lines, line{"", -1})
			}
			lines = append(lines, line{headStyle.Render("  " + strings.ToUpper(title)), -1})
		}
		v, src := m.current(k)
		val := display(v)
		if _, ok := m.pending[k.Name]; ok {
			val = accentStyle.Render(val)
		}
		lines = append(lines, line{fmt.Sprintf("%-28s %s  %s", k.Name, padRight(val, 22), mutedStyle.Render(src)), i})
	}
	selLine := 0
	for i, l := range lines {
		if l.key == m.sel {
			selLine = i
		}
	}
	room := max(6, m.height-12)
	start := min(max(0, selLine-room/2), max(0, len(lines)-room))
	end := min(len(lines), start+room)
	for _, l := range lines[start:end] {
		switch {
		case l.key == m.sel:
			b.WriteString(selStyle.Render("▸ "+l.text) + "\n")
		case l.key >= 0:
			b.WriteString("  " + l.text + "\n")
		default:
			b.WriteString(l.text + "\n")
		}
	}
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)) + "\n")
	k := config.Keys[m.sel]
	b.WriteString(boldStyle.Render(k.Name) + mutedStyle.Render("  ·  "+T("cli.config.info_default")+" "+display(k.Default)) + "\n")
	b.WriteString(config.Help(i18n.Language(), k) + "\n")
	return m.finish(&b)
}

func padRight(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}

func (m *Settings) finish(b *strings.Builder) tea.View {
	b.WriteString(mutedStyle.Render(strings.Repeat("─", m.w())) + "\n")
	switch {
	case m.asking:
		yes, no := yesNoKeys()
		b.WriteString(accentStyle.Render(T("settings.ask_save", "n", len(m.pending))) + "  " + keyStyle.Render(strings.ToUpper(yes[0])) + " " +
			T("common.yes_short") + "  " + keyStyle.Render(strings.ToUpper(no[0])) + " " + T("common.no_short") + "  " + keyStyle.Render("Esc") + " " + T("settings.keep_editing") + "\n")
	case m.notice != "":
		b.WriteString(m.notice + "\n")
	default:
		b.WriteString("\n")
	}
	if m.form == nil && !m.asking {
		k := func(key, label string) string { return keyStyle.Render(key) + " " + label }
		keys := []string{k("Enter", T("editor.key.edit")), k("R", T("settings.key.reset")), k("S", T("keys.save")), k("Q", T("editor.key.quit"))}
		if n := len(m.pending); n > 0 {
			keys = append(keys, mutedStyle.Render(T("settings.pending", "n", n)))
		}
		b.WriteString(strings.Join(keys, "  "))
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}
