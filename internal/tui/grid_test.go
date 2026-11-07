package tui

import (
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Git Bash in Windows Terminal may not pass WT_SESSION, but it always sets TERM.
func TestPlainGlyphsOnlyWithoutAnyTerminalVariable(t *testing.T) {
	if runtime.GOOS != "windows" {
		if wantsPlainGlyphs() {
			t.Fatal("only Windows has a legacy console")
		}
		return
	}
	for _, name := range []string{"WT_SESSION", "TERM_PROGRAM", "TERM", "COLORTERM"} {
		t.Setenv(name, "")
	}
	if !wantsPlainGlyphs() {
		t.Fatal("a console with no terminal variables is the legacy one")
	}
	t.Setenv("TERM", "xterm-256color")
	if wantsPlainGlyphs() {
		t.Fatal("TERM means a modern terminal")
	}
}

func TestGridKeepsColumnsWithWideAndCombiningText(t *testing.T) {
	g := newGrid(12, 1, rBg)
	if x := g.text(0, 0, "José", style{}); x != 4 {
		t.Fatalf("accents take one cell each, ended at %d", x)
	}
	if x := g.text(4, 0, "é", style{}); x != 5 {
		t.Fatalf("a combining mark joins its letter, ended at %d", x)
	}
	if x := g.text(5, 0, "李明", style{}); x != 9 {
		t.Fatalf("wide glyphs take two cells, ended at %d", x)
	}
	if w := ansi.StringWidth(g.String()); w != 12 {
		t.Fatalf("the row must stay 12 cells wide, got %d", w)
	}
}

func TestGridDropsWhatFallsOutside(t *testing.T) {
	g := newGrid(4, 2, rBg)
	g.text(2, 1, "abcdef", style{})
	g.put(-1, 0, "x", style{})
	g.fill(3, 1, 9, 9, rSurface)
	for i, l := range strings.Split(g.String(), "\n") {
		if w := ansi.StringWidth(l); w != 4 {
			t.Fatalf("row %d is %d cells wide", i, w)
		}
	}
}

func TestFitPadAndWrapMeasureCells(t *testing.T) {
	if got := fit("Peña García", 6); got != "Peña …" {
		t.Fatalf("fit: %q", got)
	}
	if got := pad("Ñu", 4); ansi.StringWidth(got) != 4 {
		t.Fatalf("pad: %q", got)
	}
	got := wrap("uno dos tres cuatro", 8)
	if len(got) != 3 || got[0] != "uno dos" {
		t.Fatalf("wrap: %q", got)
	}
}

func TestBigTextWidth(t *testing.T) {
	g := newGrid(30, 3, rBg)
	if x := bigText(g, 0, 0, "12:47", rInk, rBg); x != bigWidth("12:47") {
		t.Fatalf("bigWidth disagrees with bigText: %d", x)
	}
}

func TestDensityFitsTheClass(t *testing.T) {
	for _, tc := range []struct {
		w, h, n int
		want    string
	}{{120, 26, 25, "large"}, {120, 26, 60, "medium"}, {120, 26, 100, "compact"}, {80, 15, 24, "medium"}, {80, 15, 40, "compact"}} {
		if got := pickDensity(tc.w, tc.h, tc.n).name; got != tc.want {
			t.Errorf("%dx%d with %d students: %s, want %s", tc.w, tc.h, tc.n, got, tc.want)
		}
	}
}
