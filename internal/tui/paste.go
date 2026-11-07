package tui

import (
	"errors"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lisk0vian/assess/internal/exam"
)

// pasted is one question read from text pasted from a document or an AI.
type pasted struct {
	v       *questionValues
	missing bool // the right answer is not marked: it comes in incomplete
}

var (
	pasteNumber = regexp.MustCompile(`^(?:[0-9]+|[PpQq][0-9]+)\s*[.):-]\s+`)
	pasteOption = regexp.MustCompile(`^(\*?)\s*(?:\(?([a-hA-H])[.)]|[-•])\s+(.*?)\s*(\*?)$`)
	pasteAnswer = regexp.MustCompile(`(?i)^(?:r|a|ans|answer|resp|respuesta)\s*[:=]\s*(.+)$`)
	pasteTF     = regexp.MustCompile(`(?i)^(\*?)\s*(v|f|t|true|false|verdadero|falso|cierto)\s*(\*?)$`)
)

type pasteBlock struct {
	prompt  []string
	options []string
	marked  []bool
	letters []string
	tf      *bool
	tfSeen  bool
	answers []string
}

// parsePasted reads questions from text. A question starts at a numbered line
// or after a blank line; options are "a)" or "-" lines, the right ones marked
// with "*"; "R: 8" gives a short answer, and a lone "*V" or "*F" a true or false.
func parsePasted(text string) []pasted {
	var blocks []*pasteBlock
	var cur *pasteBlock
	gap := false
	for raw := range strings.SplitSeq(strings.ReplaceAll(text, "\r", ""), "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			gap = true
			continue
		}
		answerish := cur != nil && (pasteOption.MatchString(l) || pasteAnswer.MatchString(l) || pasteTF.MatchString(l))
		if cur == nil || pasteNumber.MatchString(l) || (gap && !answerish) {
			cur = &pasteBlock{}
			blocks = append(blocks, cur)
			cur.prompt = append(cur.prompt, pasteNumber.ReplaceAllString(l, ""))
			gap = false
			continue
		}
		gap = false
		switch {
		case pasteAnswer.MatchString(l):
			cur.answers = append(cur.answers, strings.TrimSpace(pasteAnswer.FindStringSubmatch(l)[1]))
		case pasteTF.MatchString(l) && len(cur.options) == 0:
			m := pasteTF.FindStringSubmatch(l)
			cur.tfSeen = true
			if m[1] != "" || m[3] != "" {
				v := strings.ContainsRune("vtc", rune(strings.ToLower(m[2])[0]))
				cur.tf = &v
			}
		case pasteOption.MatchString(l):
			m := pasteOption.FindStringSubmatch(l)
			cur.options = append(cur.options, m[3])
			cur.marked = append(cur.marked, m[1] != "" || m[4] != "")
			cur.letters = append(cur.letters, strings.ToLower(m[2]))
		case len(cur.options) > 0:
			cur.options[len(cur.options)-1] += " " + l
		default:
			cur.prompt = append(cur.prompt, l)
		}
	}
	out := make([]pasted, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.values())
	}
	return out
}

func (b *pasteBlock) values() pasted {
	v := newQuestionValues("")
	v.Prompt = strings.Join(b.prompt, "\n")
	switch {
	case len(b.options) > 0:
		// "R: b" or "R: a, c" marks options by letter too.
		for _, ans := range b.answers {
			for l := range strings.FieldsFuncSeq(strings.ToLower(ans), func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
				for i, letter := range b.letters {
					if letter != "" && letter == strings.Trim(l, ".)") {
						b.marked[i] = true
					}
				}
			}
		}
		v.Options = strings.Join(b.options, "\n")
		for i, on := range b.marked {
			if on {
				v.CorrectM = append(v.CorrectM, i)
			}
		}
		if len(v.CorrectM) > 1 {
			v.Type = exam.TypeMultiple
		} else if len(v.CorrectM) == 1 {
			v.Correct = v.CorrectM[0]
			v.CorrectM = nil
		}
		return pasted{v: v, missing: len(v.CorrectM) == 0 && v.Correct < 0}
	case b.tfSeen:
		v.Type, v.Value = exam.TypeTrueFalse, b.tf
		return pasted{v: v, missing: b.tf == nil}
	}
	v.Type = exam.TypeShort
	if len(b.answers) == 1 {
		if n, err := parseNum(b.answers[0]); err == nil {
			v.Number = fmtNum(n)
			return pasted{v: v}
		}
	}
	v.Input = "text"
	v.Accept = strings.Join(b.answers, "\n")
	return pasted{v: v, missing: len(b.answers) == 0}
}

func (m *Editor) pasteKey(k tea.KeyPressMsg) {
	switch k.String() {
	case "esc":
		m.pasting = nil
	case "enter":
		m.addPasted()
	case "alt+enter", "ctrl+j":
		m.pasting.insert("\n")
	default:
		m.pasting.key(k, true)
	}
}

// addPasted writes every question read from the pasted text, incomplete ones
// too: Review lists what they still need.
func (m *Editor) addPasted() {
	ps := parsePasted(m.pasting.value())
	if len(ps) == 0 {
		return
	}
	m.pasting = nil
	first, missing := "", 0
	for i, p := range ps {
		p.v.ID = m.d.NextQuestionID()
		q, a, err := p.v.build()
		if err == nil {
			err = m.d.SaveQuestion(q, a)
		}
		if err != nil {
			m.fail(errors.New(T("editor.paste.failed", "n", i, "err", err.Error())))
			return
		}
		if first == "" {
			first = q.ID
		}
		if p.missing {
			missing++
		}
	}
	m.selectQuestion(first)
	m.markSaved()
	m.tell(T("editor.paste.added", "n", len(ps)) + pasteMissing(missing))
}

func pasteMissing(n int) string {
	if n == 0 {
		return ""
	}
	return " · " + T("editor.paste.to_finish", "n", n)
}

func (m *Editor) drawPaste(g *grid, y, H int) {
	big := g.w >= wideCols
	lw := g.w - 4
	if big {
		lw = min(64, g.w*11/20)
	}
	b := rSurface
	ps := parsePasted(m.pasting.value())
	sheet(g, 1, y+1, lw, H-3)
	end := label(g, 3, y+2, T("editor.paste.here"), b)
	g.text(end+2, y+2, fit(T("editor.paste.hint"), lw-end-3), style{fg: rInk2, bg: b})
	fh := H - 6
	if !big {
		fh -= 3
	}
	_, cr, _ := m.pasting.rows(max(1, lw-7))
	body(g, 2, y+4, lw-2, fh, cr, func(s *grid) int {
		return m.pasting.draw(s, 1, 0, lw-4, 1, true, style{fg: rInk, bg: b})
	})
	missing := 0
	for _, p := range ps {
		if p.missing {
			missing++
		}
	}
	summary := T("editor.paste.count", "n", len(ps)) + pasteMissing(missing)
	if !big {
		if len(ps) == 0 {
			para(g, 3, y+6, lw-4, T("editor.paste.empty"), style{fg: rInk2, bg: b})
			return
		}
		g.text(3, y+H-4, fit(summary, lw-4), style{fg: rInk, bg: b, bold: true})
		return
	}
	X, c := lw+4, g.w-lw-6
	label(g, X, y+2, T("editor.paste.understood"), rBg)
	Y := y + 4
	if len(ps) == 0 {
		para(g, X, Y, c, T("editor.paste.empty"), style{fg: rInk2, bg: rBg})
		return
	}
	for _, p := range ps {
		if Y > y+H-6 {
			break
		}
		mark := "✓"
		if p.missing {
			mark = "!"
		}
		g.text(X, Y, mark, style{fg: rInk, bg: rBg, bold: true})
		g.text(X+2, Y, pad(fit(typeName(p.v.Type), 10), 10), style{fg: rInk2, bg: rBg})
		g.text(X+13, Y, fit(firstLine(p.v.Prompt), c-13), style{fg: rInk, bg: rBg})
		Y++
		if p.missing {
			for _, l := range wrap(T("editor.paste.missing"), c-4) {
				g.fill(X+2, Y, c-2, 1, rInverse)
				g.text(X+3, Y, l, style{fg: rOnInverse, bg: rInverse, bold: true})
				Y++
			}
		}
		Y++
	}
	g.text(X, y+H-5, fit(summary, c), style{fg: rInk, bg: rBg, bold: true})
	g.text(X, y+H-4, fit(T("editor.paste.nothing_yet"), c), style{fg: rInk2, bg: rBg})
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
