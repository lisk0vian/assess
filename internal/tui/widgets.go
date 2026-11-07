package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Components of DESIGN.md. Each draws on a grid in cells.

const wideCols = 100 // from here: three-row strip, big digits, side-by-side panels

// keyHint is one entry of a key bar.
type keyHint struct{ key, label string }

func keyWidth(k keyHint) int { return ansi.StringWidth(k.key) + 3 + ansi.StringWidth(k.label) + 1 }

func keyRows(w int, keys []keyHint) int {
	rows, x := 1, 1
	for _, k := range keys {
		n := keyWidth(k)
		if x+n > w {
			rows++
			x = 1
		}
		x += n
	}
	return rows
}

// keyBar draws the keys at the bottom and returns the rows it takes.
func keyBar(g *grid, keys []keyHint) int {
	rows := keyRows(g.w, keys)
	y := g.h - rows
	g.fill(0, y, g.w, rows, rSurface2)
	x := 1
	for _, k := range keys {
		if x+keyWidth(k) > g.w {
			y++
			x = 1
		}
		x = g.text(x, y, " "+k.key+" ", style{fg: rOnInverse, bg: rInverse, bold: true})
		x = g.text(x, y, " "+k.label, style{fg: rInk2, bg: rSurface2}) + 2
	}
	return rows
}

func tabBar(g *grid, y int, names []string, cur int) {
	g.fill(0, y, g.w, 1, rBg)
	x := 1
	for i, n := range names {
		st := style{fg: rInk2, bg: rBg}
		if i == cur {
			st = style{fg: rInk, bg: rSurface, bold: true}
		}
		x = g.text(x, y, " "+n+" ", st) + 1
	}
}

// label is a small heading: caps in secondary ink.
func label(g *grid, x, y int, s string, bg role) int {
	return g.text(x, y, s, style{fg: rInk2, bg: bg, bold: true})
}

// para writes text that must be read whole, wrapped to w, and returns the row after it.
func para(g *grid, x, y, w int, text string, st style) int {
	for _, l := range wrap(text, w) {
		g.text(x, y, l, st)
		y++
	}
	return y
}

// section is a heading with a hint beside it.
func section(g *grid, x, y int, title, hint string, bg role) {
	end := g.text(x, y, title, style{fg: rInk, bg: bg, bold: true})
	if hint != "" {
		g.text(end+2, y, hint, style{fg: rInk2, bg: bg})
	}
}

// sheet is the reading surface with the sheet beneath showing.
func sheet(g *grid, x, y, w, h int) {
	g.fill(x+1, y+1, w, h, rSurface2)
	g.fill(x, y, w, h, rSurface)
}

// panel is a box over the current screen with one task.
func panel(g *grid, x, y, w, h int, title string) {
	sheet(g, x, y, w, h)
	g.hline(x, y, w, rRuleStrong, rSurface)
	g.text(x+2, y+1, title, style{fg: rInk, bg: rSurface, bold: true})
}

// record draws labelled cells across a sheet, ending in a strong rule.
func record(g *grid, x, y, w int, cells [][2]string, bg role) {
	cx := x + 2
	for i, c := range cells {
		g.text(cx, y, c[0], style{fg: rInk, bg: bg, bold: true})
		g.text(cx, y+1, c[1], style{fg: rInk2, bg: bg})
		cx += max(ansi.StringWidth(c[0]), ansi.StringWidth(c[1])) + 3
		if i < len(cells)-1 && cx < x+w-2 {
			g.vline(cx-2, y, 2, rRule, bg)
		}
	}
	g.hline(x, y+2, w, rRuleStrong, bg)
}

// plate is the reversed plate: serious moments invert to ink.
func plate(g *grid, x, y, w int, title, body string) {
	g.fill(x, y, w, 2+len(wrap(body, w-4))+1, rInverse)
	g.text(x+2, y, fit(title, w-4), style{fg: rOnInverse, bg: rInverse, bold: true})
	for i, l := range wrap(body, w-4) {
		g.text(x+2, y+1+i, l, style{fg: rOnInverse2, bg: rInverse})
	}
}

// actionKey draws a key on a plate.
func plateKey(g *grid, x, y int, key, text string) int {
	x = g.text(x, y, " "+key+" ", style{fg: rInverse, bg: rOnInverse, bold: true})
	return g.text(x, y, " "+text, style{fg: rOnInverse, bg: rInverse}) + 2
}

// inputLine is an inline field with the magenta caret.
func inputLine(g *grid, x, y, w int, value string, bg role) {
	g.fill(x, y, w, 1, bg)
	end := g.text(x+1, y, fit(value, w-3), style{fg: rInk, bg: bg, bold: true})
	g.put(end, y, "▌", style{fg: rAccent, bg: bg})
}

// tally is the strikes as squares.
func tally(n, limit int) string {
	return strings.Repeat("■", min(n, limit)) + strings.Repeat("□", max(0, limit-n))
}

// tone is the answering record: ink fills as questions are answered.
func tone(g *grid, x, y, w, done, total int, fg, bg role) {
	n := 0
	if total > 0 {
		n = (w*done + total/2) / total
	}
	for i := range w {
		if i < n {
			g.put(x+i, y, "█", style{fg: fg, bg: bg})
		} else {
			g.put(x+i, y, "·", style{fg: rInk3, bg: bg})
		}
	}
}

// digits are 3×5 pixels on three rows of half cells; the top pixel row stays empty.
var digits = map[rune]string{'0': "111101101101111", '1': "010110010010111", '2': "111001111100111", '3': "111001011001111",
	'4': "101101111001001", '5': "111100111001111", '6': "111100111101111", '7': "111001010010010", '8': "111101111101111", '9': "111101111001111"}

func bigWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == ':' {
			w += 2
		} else {
			w += 4
		}
	}
	return w
}

// bigText draws digits and colons three rows tall; it returns the next column.
func bigText(g *grid, x, y int, s string, fg, bg role) int {
	st := style{fg: fg, bg: bg}
	for _, r := range s {
		p, ok := digits[r]
		if !ok {
			g.put(x, y, "▄", st)
			g.put(x, y+1, "▄", st)
			x += 2
			continue
		}
		for c := range 3 {
			for row := range 3 {
				top := row > 0 && p[(row*2-1)*3+c] == '1'
				bot := p[row*2*3+c] == '1'
				glyph := " "
				switch {
				case top && bot:
					glyph = "█"
				case top:
					glyph = "▀"
				case bot:
					glyph = "▄"
				}
				g.put(x+c, y+row, glyph, st)
			}
		}
		x += 4
	}
	return x
}

// halfBar is a bar half a cell tall, so rows never merge in any font.
func halfBar(g *grid, x, y, w int, v float64, fg, bg role) {
	n := int(max(0, min(1, v))*float64(w) + 0.5)
	for i := range n {
		g.put(x+i, y, "▄", style{fg: fg, bg: bg})
	}
}

// qrBlock draws a QR with fixed colours, so it scans in both themes.
func qrBlock(g *grid, x, y int, qr string) {
	for j, l := range strings.Split(qr, "\n") {
		i := 0
		for _, r := range l {
			g.put(x+i, y+j, string(r), style{fg: rQRLight, bg: rQRDark})
			i++
		}
	}
}

func qrSize(qr string) (w, h int) {
	lines := strings.Split(qr, "\n")
	return ansi.StringWidth(lines[0]), len(lines)
}

// codeCells draws the session code in ruled cells.
func codeCells(g *grid, x, y int, code string, bg role) {
	rule := style{fg: rRuleStrong, bg: bg}
	for i, r := range code {
		cx := x + i*6
		g.text(cx, y, "┌───┐", rule)
		g.text(cx, y+1, "│", rule)
		g.text(cx+2, y+1, string(r), style{fg: rInk, bg: bg, bold: true})
		g.text(cx+4, y+1, "│", rule)
		g.text(cx, y+2, "└───┘", rule)
	}
}

// helpPanel lists a screen's keys, from the table that handles them.
func helpPanel(g *grid, table []keyGroup) {
	wide := g.w >= wideCols
	w, h, x, y := min(96, g.w-4), 20, 0, 5
	if !wide {
		w, h, y = g.w-4, g.h-3, 1
	}
	x = (g.w - w) / 2
	b := rSurface
	panel(g, x, y, w, h, T("monitor.ui.help_title"))
	g.right(x+w-2, y+1, T("monitor.ui.help_close"), style{fg: rInk2, bg: b})
	var groups []keyGroup
	for _, kg := range table {
		if len(kg.items) > 0 {
			groups = append(groups, kg)
		}
	}
	cw := (w - 4) / max(1, len(groups))
	for c, kg := range groups {
		cx := x + 2 + c*cw
		label(g, cx, y+3, fit(kg.title, cw-1), b)
		for i, it := range kg.items {
			if y+5+i >= y+h-1 {
				break
			}
			end := g.text(cx, y+5+i, " "+it.key+" ", style{fg: rOnInverse, bg: rInverse, bold: true})
			g.text(end+1, y+5+i, fit(it.label, cx+cw-end-2), style{fg: rInk, bg: b})
		}
	}
}

// confirmBox asks yes or no on a plate; alt adds a third answer under A.
func confirmBox(g *grid, question, alt string) {
	w := min(70, g.w-4)
	x, y := (g.w-w)/2, max(4, g.h/2-3)
	g.fill(x, y, w, 5, rInverse)
	lines := wrap(question, w-4)
	for i, l := range lines[:min(2, len(lines))] {
		g.text(x+2, y+1+i, l, style{fg: rOnInverse, bg: rInverse, bold: true})
	}
	yes, no := yesNoKeys()
	kx := plateKey(g, x+2, y+3, strings.ToUpper(yes[0]), T("common.yes_short"))
	if alt != "" {
		kx = plateKey(g, kx, y+3, "A", fit(alt, w-22))
	}
	plateKey(g, kx, y+3, strings.ToUpper(no[0]), T("common.no_short"))
}
