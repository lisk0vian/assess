package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/search"
)

// PickItem is one candidate of the picker.
type PickItem struct {
	Key    string // the identifier, shown first ("A-12")
	Label  string // what it is ("Ana García")
	Detail string // context ("3B · 4 sesiones")
}

// Picker is the "Did you mean…?" list: the candidates for something the
// teacher typed, narrowed further as they type.
type Picker struct {
	title   string
	items   []PickItem
	query   []rune
	shown   []int // indexes into items
	cur     int
	top     int
	Chosen  int // index into items; -1 when cancelled
	visible int
}

// NewPicker lists items under title.
func NewPicker(title string, items []PickItem) *Picker {
	p := &Picker{title: title, items: items, Chosen: -1, visible: 10}
	p.filter()
	return p
}

func (p *Picker) filter() {
	p.shown = p.shown[:0]
	q := string(p.query)
	for i, it := range p.items {
		if q == "" || search.Score(q, it.Key, it.Label, it.Detail) > 0 {
			p.shown = append(p.shown, i)
		}
	}
	p.cur, p.top = 0, 0
}

// Init does nothing.
func (p *Picker) Init() tea.Cmd { return nil }

// Update moves the cursor, filters and chooses.
func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Height > 0 {
			p.visible = max(3, min(10, msg.Height-6))
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			p.Chosen = -1
			return p, tea.Quit
		case "enter":
			if len(p.shown) > 0 {
				p.Chosen = p.shown[p.cur]
			}
			return p, tea.Quit
		case "up", "ctrl+p":
			if p.cur > 0 {
				p.cur--
			}
		case "down", "ctrl+n", "tab":
			if p.cur < len(p.shown)-1 {
				p.cur++
			}
		case "backspace":
			if len(p.query) > 0 {
				p.query = p.query[:len(p.query)-1]
				p.filter()
			}
		default:
			if msg.Text != "" {
				p.query = append(p.query, []rune(msg.Text)...)
				p.filter()
			}
		}
		if p.cur < p.top {
			p.top = p.cur
		}
		if p.cur >= p.top+p.visible {
			p.top = p.cur - p.visible + 1
		}
	}
	return p, nil
}

// View draws the title, the filter and the visible candidates.
func (p *Picker) View() tea.View {
	var b strings.Builder
	b.WriteString(accentStyle.Render("?") + " " + boldStyle.Render(p.title) + "\n")
	filter := mutedStyle.Render(T("pick.filter")) + " " + string(p.query) + accentStyle.Render("▏")
	b.WriteString("  " + filter + "\n")
	keyW := 0
	for _, i := range p.shown {
		keyW = max(keyW, len([]rune(p.items[i].Key)))
	}
	if len(p.shown) == 0 {
		b.WriteString("  " + mutedStyle.Render(T("pick.none")) + "\n")
	}
	end := min(len(p.shown), p.top+p.visible)
	for row := p.top; row < end; row++ {
		it := p.items[p.shown[row]]
		key := it.Key + strings.Repeat(" ", keyW-len([]rune(it.Key)))
		line := key + "  " + it.Label
		if row == p.cur {
			line = accentStyle.Render("› ") + selStyle.Render(" "+line+" ")
		} else {
			line = "   " + line + " "
		}
		if it.Detail != "" {
			line += " " + mutedStyle.Render(it.Detail)
		}
		b.WriteString(line + "\n")
	}
	if len(p.shown) > end-p.top {
		b.WriteString("  " + mutedStyle.Render(T("pick.more", "from", p.top+1, "to", end, "total", len(p.shown))) + "\n")
	}
	k := func(key, label string) string { return keyStyle.Render(key) + " " + label }
	b.WriteString(mutedStyle.Render("  ") + strings.Join([]string{k("↑↓", T("pick.key.move")), k("Enter", T("pick.key.choose")), k("Esc", T("pick.key.cancel"))}, "   "))
	return tea.NewView(b.String())
}

// Pick runs the picker and returns the chosen index, or -1.
func Pick(title string, items []PickItem) (int, error) {
	p := NewPicker(title, items)
	if _, err := tea.NewProgram(p).Run(); err != nil {
		return -1, err
	}
	return p.Chosen, nil
}
