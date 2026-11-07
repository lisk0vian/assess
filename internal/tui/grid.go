package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// grid is a screen as cells, the same model as design/canvas.html. Painting
// every cell keeps backgrounds whole, which nested Render calls cannot do.
type grid struct {
	w, h int
	c    []cell
}

type cell struct {
	g string // "" is the second half of a wide glyph
	cellStyle
}

type cellStyle struct {
	fg, bg role
	bold   bool
}

type style = cellStyle

func newGrid(w, h int, bg role) *grid {
	g := &grid{w: w, h: h, c: make([]cell, w*h)}
	for i := range g.c {
		g.c[i] = cell{g: " ", cellStyle: cellStyle{fg: rInk, bg: bg}}
	}
	return g
}

func (g *grid) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return nil
	}
	return &g.c[y*g.w+x]
}

// put writes one glyph; anything outside the grid is dropped.
func (g *grid) put(x, y int, glyph string, s style) {
	if c := g.at(x, y); c != nil {
		c.g, c.cellStyle = glyph, s
	}
}

// text writes s from x and returns the column after it. Width comes from
// the glyphs, so accents and CJK names keep their columns.
func (g *grid) text(x, y int, s string, st style) int {
	for _, r := range s {
		glyph := string(r)
		switch ansi.StringWidth(glyph) {
		case 0:
			if c := g.at(x-1, y); c != nil {
				c.g += glyph
			}
		case 2:
			g.put(x, y, glyph, st)
			g.put(x+1, y, "", st)
			x += 2
		default:
			g.put(x, y, glyph, st)
			x++
		}
	}
	return x
}

// right writes s so that it ends just before column end.
func (g *grid) right(end, y int, s string, st style) int {
	return g.text(end-ansi.StringWidth(s), y, s, st)
}

func (g *grid) fill(x, y, w, h int, bg role) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			g.put(i, j, " ", style{fg: rInk, bg: bg})
		}
	}
}

func (g *grid) hline(x, y, w int, fg, bg role) {
	for i := x; i < x+w; i++ {
		g.put(i, y, "─", style{fg: fg, bg: bg})
	}
}

func (g *grid) vline(x, y, h int, fg, bg role) {
	for j := y; j < y+h; j++ {
		g.put(x, j, "│", style{fg: fg, bg: bg})
	}
}

// String paints the grid, one styled run at a time.
func (g *grid) String() string {
	var b strings.Builder
	for y := range g.h {
		if y > 0 {
			b.WriteByte('\n')
		}
		var run strings.Builder
		var cur cellStyle
		flush := func() {
			if run.Len() > 0 {
				b.WriteString(render(cur, run.String()))
				run.Reset()
			}
		}
		for x := range g.w {
			c := g.c[y*g.w+x]
			if c.cellStyle != cur {
				flush()
				cur = c.cellStyle
			}
			if plainGlyphs {
				if alt, ok := glyphFallback[c.g]; ok {
					c.g = alt
				}
			}
			run.WriteString(c.g)
		}
		flush()
	}
	return b.String()
}

var styleCache = map[cellStyle]lipgloss.Style{}

func render(s cellStyle, text string) string {
	st, ok := styleCache[s]
	if !ok {
		st = lipgloss.NewStyle().Foreground(palette[s.fg]).Background(palette[s.bg]).Bold(s.bold)
		styleCache[s] = st
	}
	return st.Render(text)
}

// fit cuts s to n cells with an ellipsis.
func fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

// pad fits s to exactly n cells.
func pad(s string, n int) string {
	s = fit(s, n)
	return s + strings.Repeat(" ", max(0, n-ansi.StringWidth(s)))
}

// wrap breaks s into lines of at most n cells.
func wrap(s string, n int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case ansi.StringWidth(line)+1+ansi.StringWidth(word) > n:
			out = append(out, line)
			line = word
		default:
			line += " " + word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
