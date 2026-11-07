package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// textField is an editable text drawn on a grid; huh's fields draw their own
// strings, which cannot sit on painted cells.
type textField struct {
	r   []rune
	pos int
}

func newField(s string) textField {
	r := []rune(s)
	return textField{r: r, pos: len(r)}
}

func (f *textField) value() string { return string(f.r) }

func (f *textField) insert(s string) {
	in := []rune(strings.ReplaceAll(s, "\r\n", "\n"))
	f.r = slices.Insert(f.r, f.pos, in...)
	f.pos += len(in)
}

func (f *textField) lineStart() int {
	i := f.pos
	for i > 0 && f.r[i-1] != '\n' {
		i--
	}
	return i
}

func (f *textField) lineEnd() int {
	i := f.pos
	for i < len(f.r) && f.r[i] != '\n' {
		i++
	}
	return i
}

// vertical moves the caret one written line, keeping its column. It reports
// false at the first or last line, so the caller can leave the field.
// shortcut: moves by written line, not by wrapped row; a long paragraph needs Home or End.
func (f *textField) vertical(delta int) bool {
	start, end := f.lineStart(), f.lineEnd()
	col := f.pos - start
	if delta < 0 {
		if start == 0 {
			return false
		}
		f.pos = start - 1
		f.pos = min(f.lineStart()+col, f.lineEnd())
		return true
	}
	if end == len(f.r) {
		return false
	}
	f.pos = end + 1
	f.pos = min(f.pos+col, f.lineEnd())
	return true
}

// key applies a keypress and reports whether the field used it.
func (f *textField) key(k tea.KeyPressMsg, multi bool) bool {
	switch k.String() {
	case "left":
		f.pos = max(0, f.pos-1)
	case "right":
		f.pos = min(len(f.r), f.pos+1)
	case "home":
		f.pos = f.lineStart()
	case "end":
		f.pos = f.lineEnd()
	case "backspace":
		if f.pos > 0 {
			f.r = slices.Delete(f.r, f.pos-1, f.pos)
			f.pos--
		}
	case "delete":
		if f.pos < len(f.r) {
			f.r = slices.Delete(f.r, f.pos, f.pos+1)
		}
	case "up", "down":
		if !multi {
			return false
		}
		if k.String() == "up" {
			return f.vertical(-1)
		}
		return f.vertical(1)
	case "enter":
		if !multi {
			return false
		}
		f.insert("\n")
	default:
		if k.Text == "" {
			return false
		}
		f.insert(k.Text)
	}
	return true
}

// rows breaks the text to width cells, at a space when it can, and finds the
// caret in the result.
func (f *textField) rows(width int) (rows []string, caretRow, caretCol int) {
	cells := func(from, to int) int { return ansi.StringWidth(string(f.r[from:to])) }
	var starts []int
	start, space := 0, -1
	add := func(end int) {
		rows = append(rows, string(f.r[start:end]))
		starts = append(starts, start)
	}
	for i := 0; i <= len(f.r); i++ {
		if i == len(f.r) || f.r[i] == '\n' {
			add(i)
			start, space = i+1, -1
			continue
		}
		if cells(start, i+1) > width {
			if space >= start {
				add(space + 1)
				start = space + 1
			} else {
				add(i)
				start = i
			}
			space = -1
		}
		if f.r[i] == ' ' {
			space = i
		}
	}
	for i, s := range starts {
		if s <= f.pos {
			caretRow, caretCol = i, cells(s, f.pos)
		}
	}
	return rows, caretRow, caretCol
}

// draw paints the field from y and returns the rows it took, at least minRows.
func (f *textField) draw(g *grid, x, y, w, minRows int, focus bool, st style) int {
	rows, cr, cc := f.rows(max(1, w-3))
	n := max(minRows, len(rows))
	g.fill(x, y, w, n, st.bg)
	for i, l := range rows {
		g.text(x+1, y+i, l, st)
	}
	if focus {
		g.put(x+1+cc, y+cr, "▌", style{fg: rAccent, bg: st.bg})
	}
	return n
}
