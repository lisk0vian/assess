package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/i18n"
)

// SettingChange is one setting changed on the Settings screen.
type SettingChange struct {
	Key, Value string
	Reset      bool // back to the default (the line is commented out again)
}

// Settings edits the settings file: every key, grouped as in the file. Nothing
// is written here; the caller saves Changes when the screen closes.
type Settings struct {
	cfg           *config.Config
	groups        []string
	group, sel    int
	width, height int
	pending       map[string]SettingChange
	order         []string // keys in the order they were changed

	editing *config.Key
	options []huh.Option[string] // nil for a typed value
	choice  int
	field   textField
	problem string

	notice   string
	showHelp bool

	// Changes are the changes, in order, once the screen closes.
	Changes []SettingChange
}

// NewSettings opens the Settings screen on the loaded settings.
func NewSettings(cfg *config.Config) *Settings {
	return &Settings{cfg: cfg, groups: config.Groups(), width: 120, height: 36, pending: map[string]SettingChange{}}
}

// Init asks for the terminal's background, which picks the theme.
func (m *Settings) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m *Settings) keys() []config.Key {
	var out []config.Key
	for _, k := range config.Keys {
		if k.Group() == m.groups[m.group] {
			out = append(out, k)
		}
	}
	return out
}

// current is the value shown for k: pending, or the effective one.
func (m *Settings) current(k config.Key) (string, string) {
	if ch, ok := m.pending[k.Name]; ok {
		if ch.Reset {
			return k.Default, T("settings.will_reset")
		}
		return ch.Value, T("settings.changed")
	}
	v := m.cfg.Get(k.Name)
	if v.Source == config.FromFile {
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

// Update handles the list keys and the open editor.
func (m *Settings) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case tea.PasteMsg:
		if m.editing != nil && m.options == nil {
			m.field.insert(strings.ReplaceAll(msg.Content, "\n", " "))
		}
	case tea.KeyPressMsg:
		switch k := msg.String(); {
		case m.showHelp:
			if k == "?" || k == "esc" {
				m.showHelp = false
			}
		case m.editing != nil:
			m.editKey(msg)
		default:
			return m, m.listKey(k)
		}
	}
	return m, nil
}

func (m *Settings) listKey(k string) tea.Cmd {
	keys := m.keys()
	switch k {
	case "q", "Q", "esc", "ctrl+c", "s", "S":
		m.Changes = nil
		for _, name := range m.order {
			m.Changes = append(m.Changes, m.pending[name])
		}
		return tea.Quit
	case "up", "k":
		m.sel = max(0, m.sel-1)
	case "down", "j":
		m.sel = min(len(keys)-1, m.sel+1)
	case "left":
		m.group, m.sel = max(0, m.group-1), 0
	case "right":
		m.group, m.sel = min(len(m.groups)-1, m.group+1), 0
	case "?":
		m.showHelp = true
	case "enter":
		m.edit(keys[m.sel])
	case "r", "R":
		kk := keys[m.sel]
		m.change(SettingChange{Key: kk.Name, Reset: true})
		m.notice = T("settings.reset_notice", "key", kk.Name, "value", display(kk.Default))
	}
	return nil
}

func isLangKey(k config.Key) bool { return k.Name == "language" || k.Name == "student_language" }

func (m *Settings) edit(k config.Key) {
	cur, _ := m.current(k)
	m.editing, m.options, m.choice, m.problem, m.notice = &k, nil, 0, "", ""
	switch {
	case isLangKey(k):
		m.options = languageOptions(cur)
		if k.Name == "student_language" {
			m.options = append([]huh.Option[string]{huh.NewOption(T("settings.same_as_terminal"), "")}, m.options...)
		}
	case k.Kind == config.Choice:
		for _, c := range k.Choices {
			m.options = append(m.options, huh.NewOption(choiceLabel(k, c), c))
		}
	case k.Kind == config.Bool:
		m.options = []huh.Option[string]{huh.NewOption(T("common.yes"), "true"), huh.NewOption(T("common.no"), "false")}
	default:
		m.field = newField(cur)
	}
	for i, o := range m.options {
		if o.Value == cur {
			m.choice = i
		}
	}
}

func (m *Settings) editKey(msg tea.KeyPressMsg) {
	k := *m.editing
	s := msg.String()
	switch {
	case s == "esc" || s == "ctrl+c":
		m.editing = nil
	case s == "enter":
		m.confirm(k)
	case m.options == nil:
		m.field.key(msg, false)
		m.problem = ""
	case s == "left":
		m.choice = max(0, m.choice-1)
	case s == "right":
		m.choice = min(len(m.options)-1, m.choice+1)
	}
}

func (m *Settings) confirm(k config.Key) {
	var v string
	if m.options != nil {
		v = m.options[m.choice].Value
	} else {
		v = strings.TrimSpace(m.field.value())
		if v != "" || k.Kind != config.String {
			if err := k.Validate(v); err != nil {
				m.problem = err.Error()
				return
			}
		}
	}
	m.editing = nil
	if old, _ := m.current(k); old == v {
		return
	}
	m.change(SettingChange{Key: k.Name, Value: v})
	m.notice = T("settings.changed_notice", "key", k.Name, "value", display(v))
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

func (m *Settings) groupName(g string) string {
	if g == "" {
		return T("settings.general")
	}
	return T("config.group." + g)
}

func (m *Settings) keyTable() []keyGroup {
	k := func(key, label string, bar bool) keyItem { return keyItem{keyHint{key, label}, bar} }
	if m.editing != nil {
		items := []keyItem{k("Enter", T("keys.save"), true), k("Esc", T("hub.key.back"), true)}
		if m.options != nil {
			items = append([]keyItem{k("←→", T("editor.ui.key_option"), true)}, items...)
		}
		return []keyGroup{{m.editing.Name, items}}
	}
	return []keyGroup{{T("hub.vault_act.settings"), []keyItem{k("↑↓", T("settings.key.setting"), true), k("←→", T("settings.key.group"), true), k("Enter", T("editor.key.edit"), true),
		k("R", T("settings.key.reset"), true), k("?", T("monitor.ui.key_help"), false), k("Esc", T("settings.key.save_back"), true)}}}
}

// View draws the groups, their settings and the help of the selected one.
func (m *Settings) View() tea.View {
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

func (m *Settings) screen(g *grid) {
	big := g.w >= wideCols
	b := rSurface
	g.fill(0, 0, g.w, 2, b)
	g.text(1, 0, T("hub.vault_act.settings"), style{fg: rInk, bg: b, bold: true})
	g.text(1, 1, fit(m.cfg.Path, g.w-2), style{fg: rInk2, bg: b})
	g.hline(0, 2, g.w, rRuleStrong, rBg)
	var bar []keyHint
	for _, it := range m.keyTable()[0].items {
		if it.bar {
			bar = append(bar, it.keyHint)
		}
	}
	bottom := g.h - keyBar(g, bar)
	y, H := 3, bottom-3
	rw := 18
	if big {
		rw = 22
	}
	g.fill(rw, y, g.w-rw, H, b)
	for i, name := range m.groups {
		if y+1+i < bottom {
			on := i == m.group
			rowBg := selRow(g, 0, y+1+i, rw, on, rBg)
			g.text(2, y+1+i, fit(m.groupName(name), rw-3), nameStyle(on, rowBg))
		}
	}
	g.vline(rw, y, H, rRuleStrong, b)
	X, W := rw+1, g.w-rw-1
	if big {
		W = min(W, 66)
	}
	keys := m.keys()
	m.sel = min(m.sel, len(keys)-1)
	vx := W * 5 / 9
	label(g, X+2, y+1, pad(T("settings.col.setting"), vx-2)+T("settings.col.value"), b)
	g.hline(X, y+2, W, rRuleStrong, b)
	room := H - 3
	if !big {
		room -= 5
	}
	top := window(m.sel, len(keys), room)
	for i := range min(room, len(keys)-top) {
		k := keys[top+i]
		on, Y := top+i == m.sel, y+3+i
		rowBg := selRow(g, X, Y, W, on, b)
		g.text(X+2, Y, fit(k.Name, vx-3), nameStyle(on, rowBg))
		v, _ := m.current(k)
		g.text(X+vx, Y, fit(display(v), W-vx-3), style{fg: rInk, bg: rowBg, bold: true})
		if _, changed := m.pending[k.Name]; changed {
			g.right(X+W-1, Y, "●", style{fg: rInk, bg: rowBg})
		}
	}
	k := keys[m.sel]
	_, src := m.current(k)
	if big {
		P, c := X+W+2, g.w-X-W-4
		g.vline(P-2, y+1, H-2, rRule, b)
		Y := para(g, P, y+1, c, k.Name, style{fg: rInk, bg: b, bold: true})
		Y = para(g, P, Y+1, c, config.Help(i18n.Language(), k), style{fg: rInk, bg: b}) + 1
		Y = para(g, P, Y, c, T("cli.config.info_default")+" "+display(k.Default), style{fg: rInk2, bg: b})
		para(g, P, Y, c, src, style{fg: rInk2, bg: b})
	} else {
		para(g, X+2, bottom-5, W-4, config.Help(i18n.Language(), k), style{fg: rInk2, bg: b})
	}
	if m.notice != "" {
		g.text(X+2, bottom-1, fit(m.notice, g.w-X-4), style{fg: rInk, bg: b, bold: true})
	}
	if m.editing != nil {
		m.drawEdit(g)
	}
	if m.showHelp {
		helpPanel(g, m.keyTable())
	}
}

func (m *Settings) drawEdit(g *grid) {
	k := *m.editing
	w := min(70, g.w-4)
	help := wrap(config.Help(i18n.Language(), k), w-4)
	rows := len(help)
	h := min(rows+8, g.h-6)
	x, y := (g.w-w)/2, max(3, (g.h-h)/2-1)
	panel(g, x, y, w, h, fit(k.Name, w-4))
	b := rSurface
	Y := y + 3
	for _, l := range help[:min(rows, h-7)] {
		g.text(x+2, Y, l, style{fg: rInk2, bg: b})
		Y++
	}
	Y++
	if m.options != nil {
		names := make([]string, len(m.options))
		for i, o := range m.options {
			names[i] = o.Key
		}
		sub := newGrid(w-4, 3, b)
		chips(sub, 1, 0, names, m.choice, true, b)
		for j := range min(2, y+h-Y) {
			copy(g.c[(Y+j)*g.w+x+2:(Y+j)*g.w+x+w-2], sub.c[j*(w-4):(j+1)*(w-4)])
		}
		return
	}
	m.field.draw(g, x+2, Y, w-4, 1, true, style{fg: rInk, bg: rSurface2, bold: true})
	if m.problem != "" {
		g.fill(x, Y+1, w, 1, rInverse)
		g.text(x+2, Y+1, fit(m.problem, w-4), style{fg: rOnInverse, bg: rInverse, bold: true})
	}
}
