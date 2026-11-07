package tui

import (
	"image/color"
	"os"
	"runtime"

	"charm.land/lipgloss/v2"
)

// role is a colour by purpose; see DESIGN.md. Screens never name a colour,
// so both themes and 16-colour terminals work from one drawing.
type role uint8

const (
	rBg role = iota
	rSurface
	rSurface2
	rInk
	rInk2
	rInk3
	rRule
	rRuleStrong
	rAccent
	rAccentInk
	rOnAccent
	rInverse
	rOnInverse
	rOnInverse2
	rQRLight
	rQRDark
	roleCount
)

// The Mesa de luz tokens of web/src/styles/tokens.css.
var (
	lightPalette = [roleCount]string{"#e8e6e1", "#f8f7f4", "#f0efeb", "#141414", "#5b5a56", "#8f8d88", "#cdcbc5", "#141414",
		"#ec2a8a", "#a30e58", "#ffffff", "#141414", "#f8f7f4", "#c9c7c1", "#f8f7f4", "#141414"}
	darkPalette = [roleCount]string{"#111110", "#1c1c1a", "#232321", "#ecebe6", "#a7a59f", "#6f6d68", "#353431", "#bcbab4",
		"#ff4fa3", "#ff7ab9", "#111110", "#ecebe6", "#141413", "#4a4945", "#f8f7f4", "#141414"}
)

var palette = colors(darkPalette)

func colors(hex [roleCount]string) (out [roleCount]color.Color) {
	for i, h := range hex {
		out[i] = lipgloss.Color(h)
	}
	return out
}

// setDark picks the theme from the terminal's background.
func setDark(dark bool) {
	if dark {
		palette = colors(darkPalette)
	} else {
		palette = colors(lightPalette)
	}
	styleCache = map[cellStyle]lipgloss.Style{}
}

// plainGlyphs swaps the glyphs some consoles lack for ones they have.
var plainGlyphs = wantsPlainGlyphs()

// shortcut: a console with no terminal variables counts as the legacy one, so PowerShell handed to
// Windows Terminal as default terminal still gets plain glyphs; add a real probe if it is reported.
func wantsPlainGlyphs() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	for _, name := range []string{"WT_SESSION", "TERM_PROGRAM", "TERM", "COLORTERM"} {
		if os.Getenv(name) != "" {
			return false
		}
	}
	return true
}

var glyphFallback = map[string]string{"✓": "v", "⇧": "^", "↩": "<", "╱": "/", "●": "*", "○": "o", "□": ".", "▸": ">",
	"↑": "^", "↓": "v", "←": "<", "→": ">", "▁": "▄", "▂": "▄", "▃": "▄", "▅": "█", "▆": "█", "▇": "█"}

// Styles of the panels not yet drawn on the grid.
var (
	accent = lipgloss.Color("#ec2a8a")
	muted  = lipgloss.Color("#8f8d88")

	brandStyle  = lipgloss.NewStyle().Bold(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	keyStyle    = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	selStyle    = lipgloss.NewStyle().Bold(true).Reverse(true)
	headStyle   = lipgloss.NewStyle().Foreground(muted).Bold(true)
)
