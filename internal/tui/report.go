package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/barchart"
	"github.com/charmbracelet/x/ansi"

	"github.com/lisk0vian/assess/internal/report"
)

// Views of the report.
const (
	repSummary = iota
	repQuestions
	repIncidents
	repEvidence
	repHistory
	repCount
)

var repKeys = [repCount]string{"report.view.summary", "report.view.questions", "report.view.incidents", "report.view.evidence", "report.view.history"}

var (
	barStyle  = lipgloss.NewStyle().Foreground(accent)
	warnStyle = lipgloss.NewStyle().Foreground(muted)
)

// Report is the read-only panel of a finished session.
type Report struct {
	r             *report.Report
	view, scroll  int
	sel           int  // cursor in the Questions and Evidence views
	byRate        bool // questions sorted by difficulty
	width, height int
	idle          time.Duration // closes after this long without input; zero means never
	lastInput     time.Time
	// TimedOut reports that the panel closed itself after being idle.
	TimedOut bool
}

type reportTick time.Time

// NewReport builds the report panel. With idle > 0 it closes itself after
// that long without input, so personal data does not stay on screen.
func NewReport(r *report.Report, idle time.Duration) *Report {
	return &Report{r: r, idle: idle, lastInput: time.Now(), width: 120, height: 36}
}

func (m *Report) tick() tea.Cmd {
	if m.idle <= 0 {
		return nil
	}
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return reportTick(t) })
}

// Init starts the idle clock.
func (m *Report) Init() tea.Cmd { return m.tick() }

// Update handles keys, resizes and the idle clock.
func (m *Report) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case reportTick:
		if time.Since(m.lastInput) >= m.idle {
			m.TimedOut = true
			return m, tea.Quit
		}
		return m, m.tick()
	case tea.KeyPressMsg:
		m.lastInput = time.Now()
		page := max(1, m.height-6)
		switch k := msg.String(); k {
		case "q", "Q", "esc", "ctrl+c":
			return m, tea.Quit
		case "right", "l", "tab":
			m.view, m.scroll, m.sel = (m.view+1)%repCount, 0, 0
		case "left", "h", "shift+tab":
			m.view, m.scroll, m.sel = (m.view+repCount-1)%repCount, 0, 0
		case "down", "j":
			if m.listView() {
				m.sel++
			} else {
				m.scroll++
			}
		case "up", "k":
			if m.listView() {
				m.sel = max(0, m.sel-1)
			} else {
				m.scroll--
			}
		case "o", "O":
			if m.view == repQuestions {
				m.byRate = !m.byRate
			}
		case "pgdown", "space":
			m.scroll += page
		case "pgup":
			m.scroll -= page
		case "home", "g":
			m.scroll = 0
		case "end", "G":
			m.scroll = math.MaxInt32
		default:
			if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= repCount {
				m.view, m.scroll, m.sel = n-1, 0, 0
			}
		}
	}
	return m, nil
}

// listView: ↑↓ move a cursor here instead of scrolling.
func (m *Report) listView() bool { return m.view == repQuestions || m.view == repEvidence }

func (m *Report) w() int { return max(72, min(m.width, 140)) }

func (m *Report) header() string {
	s := m.r.Session
	return brandStyle.Render("ASSESS") + mutedStyle.Render("  ·  "+T("report.name")+"  ·  ") + s.ExamTitle +
		mutedStyle.Render("  ·  "+T("report.session", "code", s.Code)+"  ·  "+span(s.StartedAt, s.EndedAt))
}

func (m *Report) body(view, w int) []string {
	switch view {
	case repQuestions:
		return m.questions(w)
	case repIncidents:
		return m.incidents(w)
	case repEvidence:
		return m.evidence()
	case repHistory:
		return m.history()
	default:
		return m.summary()
	}
}

// ReportText renders every view as plain text, for pipes and files.
func ReportText(r *report.Report, width int) string {
	m := NewReport(r, 0)
	m.width = width
	var b strings.Builder
	b.WriteString(ansi.Strip(m.header()) + "\n")
	for v := range repCount {
		b.WriteString("\n== " + strings.ToUpper(T(repKeys[v])) + " ==\n")
		for _, l := range m.body(v, m.w()) {
			b.WriteString(strings.TrimRight(ansi.Strip(l), " ") + "\n")
		}
	}
	return b.String()
}

// ---------------- views ----------------

func (m *Report) summary() []string {
	s := m.r.Summary
	out := []string{
		fmt.Sprintf("  %s   %s   %s   %s", T("report.n.students", "n", s.Students), T("report.n.submitted", "n", s.Submitted),
			T("report.n.annulled", "n", s.Annulled), T("report.n.withdrawn", "n", s.Withdrawn)),
		"",
	}
	if s.Submitted == 0 {
		return append(out, mutedStyle.Render("  "+T("report.nobody_submitted")))
	}
	out = append(out,
		"  "+T("report.stats", "mean", boldStyle.Render(pct(s.Mean)), "median", boldStyle.Render(pct(s.Median)), "sd", num(s.StdDev),
			"min", pct(s.Min), "max", pct(s.Max), "time", minutes(s.MedianMinutes)),
		"",
		headStyle.Render("  "+T("report.scores.title"))+mutedStyle.Render(" · "+T("report.scores.hint")),
	)
	out = append(out, histChart(m.r.Scores, func(b report.Bin) string { return fmt.Sprintf("%g-%g", b.From, b.To) })...)
	out = append(out, "", headStyle.Render("  "+T("report.times.title"))+mutedStyle.Render(" · "+T("report.times.hint")))
	out = append(out, histChart(m.r.Times, func(b report.Bin) string { return fmt.Sprintf("%g-%g", b.From, b.To) })...)

	out = append(out, "", headStyle.Render("  "+T("report.review.title"))+mutedStyle.Render(" · "+T("report.review.hint")))
	n := 0
	for _, st := range m.r.Students {
		if st.Strikes == 0 && st.AwayHits == 0 && st.Pairs == 0 {
			continue
		}
		n++
		var why []string
		if st.Strikes > 0 {
			why = append(why, T("report.n.strikes", "n", st.Strikes))
		}
		if st.Pairs > 0 {
			why = append(why, T("report.n.pairs", "n", st.Pairs))
		}
		if st.AwayHits > 0 {
			why = append(why, T("report.n.away", "n", st.AwayHits))
		}
		score := "—"
		if st.Percent != nil {
			score = pct(*st.Percent)
		}
		out = append(out, fmt.Sprintf("  %-26s %-10s %6s   %s", cut(st.Name, 26), stateName(st.State), score, strings.Join(why, " · ")))
	}
	if n == 0 {
		out = append(out, mutedStyle.Render("  "+T("report.review.none")))
	}
	return out
}

func (m *Report) questions(w int) []string {
	qs := m.r.Questions
	out := []string{headStyle.Render("  "+T("report.rates.title")) + mutedStyle.Render(" · "+T("report.rates.hint"))}
	data := make([]barchart.BarData, 0, len(qs))
	labelW := len([]rune(T("monitor.col.question")))
	for _, q := range qs {
		labelW = max(labelW, len(q.ID))
	}
	for _, q := range qs {
		data = append(data, barchart.BarData{Label: fmt.Sprintf("%-*s ", labelW, q.ID),
			Values: []barchart.BarValue{{Name: q.ID, Value: q.Rate * 100, Style: barStyle}}})
	}
	if len(data) > 0 {
		chartW := min(w-12, 90)
		// Horizontal last: it measures the labels of the data already pushed.
		ch := barchart.New(chartW, len(data), barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(100),
			barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(1), barchart.WithBarGap(0), barchart.WithStyles(mutedStyle, mutedStyle),
			barchart.WithDataSet(data), barchart.WithHorizontalBars())
		ch.Draw()
		lines := strings.Split(ch.View(), "\n")
		for i, l := range lines {
			if i < len(qs) {
				l = lipgloss.NewStyle().Width(chartW).Render(l) + " " + pct(qs[i].Rate*100)
			}
			out = append(out, "  "+l)
		}
	}
	out = append(out, "", headStyle.Render(fmt.Sprintf("  %-*s  %-10s %8s %9s %8s   %s", labelW, T("monitor.col.question"), T("editor.col.type"), T("report.col.right"), T("report.col.blank"), T("report.col.discrimination"), T("editor.col.prompt"))))
	for _, q := range qs {
		blank, disc := "—", "—"
		if q.Seen > 0 {
			blank = pct(float64(q.Seen-q.Answered) / float64(q.Seen) * 100)
		}
		if q.Discrimination != nil {
			disc = num(*q.Discrimination)
		}
		rate := "—"
		if q.Seen > 0 {
			rate = pct(q.Rate * 100)
		}
		out = append(out, fmt.Sprintf("  %-*s  %-10s %8s %9s %8s   %s", labelW, q.ID, typeName(q.Type), rate, blank, disc, cut(q.Prompt, max(20, w-60))))
		var notes []string
		switch {
		case q.Seen == 0:
			notes = append(notes, mutedStyle.Render(T("report.note.unseen")))
		case q.Discrimination != nil && *q.Discrimination < 0:
			notes = append(notes, accentStyle.Render(T("report.note.negative")))
		case q.Discrimination != nil && *q.Discrimination < 0.15 && q.Rate > 0 && q.Rate < 1:
			notes = append(notes, mutedStyle.Render(T("report.note.weak")))
		}
		if q.Leak {
			notes = append(notes, accentStyle.Render(T("report.note.leak")))
		}
		if q.AwayHits > 0 {
			notes = append(notes, accentStyle.Render(T("report.note.away", "n", q.AwayHits)))
		}
		if q.Voided > 0 {
			notes = append(notes, mutedStyle.Render(T("report.note.voided", "students", T("report.n.students", "n", q.Voided))))
		}
		if len(notes) > 0 {
			out = append(out, strings.Repeat(" ", labelW+4)+"↳ "+strings.Join(notes, mutedStyle.Render(" · ")))
		}
	}
	return out
}

func (m *Report) incidents(w int) []string {
	inc := m.r.Incidents
	warn, high := 0, 0
	for _, k := range inc.ByKind {
		warn += k.Warn
		high += k.High
	}
	out := []string{"  " + T("report.incidents.counts", "warn", T("report.n.warnings", "n", warn), "high", T("report.n.serious", "n", high),
		"strikes", inc.Strikes, "reverted", T("report.n.reverted", "n", inc.Reverted), "time", minutes(inc.TimeLostMin),
		"voided", T("report.n.voided", "n", inc.VoidedAnswers)), ""}
	if warn+high == 0 {
		return append(out, mutedStyle.Render("  "+T("report.incidents.none")))
	}
	out = append(out, headStyle.Render("  "+T("report.timeline.title"))+mutedStyle.Render(" · "+T("report.timeline.step", "n", inc.Step)+" · ")+
		warnStyle.Render("█")+" "+T("report.timeline.warn")+"  "+barStyle.Render("█")+" "+T("report.timeline.high"))
	out = append(out, timelineChart(inc, w)...)
	out = append(out, "", headStyle.Render(fmt.Sprintf("  %-34s %7s %7s", T("report.col.kind"), T("report.col.warnings"), T("report.col.serious"))))
	for _, k := range inc.ByKind {
		out = append(out, fmt.Sprintf("  %-34s %7d %7d", signalName(k.Kind), k.Warn, k.High))
	}
	return out
}

func (m *Report) evidence() []string {
	out := []string{mutedStyle.Render("  " + T("report.evidence.disclaimer")), "",
		headStyle.Render("  "+T("report.pairs.title")) + mutedStyle.Render(" · "+T("report.pairs.hint"))}
	if len(m.r.Pairs) == 0 {
		out = append(out, mutedStyle.Render("  "+T("report.pairs.none")))
	}
	for _, p := range m.r.Pairs {
		level := mutedStyle.Render(fmt.Sprintf("%-5s", T("report.pairs.medium")))
		if p.Level == report.LevelHigh {
			level = accentStyle.Render(fmt.Sprintf("%-5s", T("report.pairs.high")))
		}
		line := fmt.Sprintf("  %s  %s ↔ %s", level, boldStyle.Render(p.Students[0].Name), boldStyle.Render(p.Students[1].Name))
		var why []string
		if p.SameWrong > 0 {
			why = append(why, T("report.pairs.same_wrong", "same", p.SameWrong, "both", p.BothWrong, "expected", num(p.Expected), "odds", odds(p.P)))
		}
		if p.Synced > 0 {
			why = append(why, T("report.pairs.synced", "answers", T("report.n.saved_answers", "n", p.Synced)))
		}
		out = append(out, line, "         "+strings.Join(why, mutedStyle.Render(" · ")))
		if len(p.Questions) > 0 {
			out = append(out, mutedStyle.Render("         "+T("report.pairs.questions", "list", strings.Join(p.Questions, " "))))
		}
	}
	out = append(out, "", headStyle.Render("  "+T("monitor.away.title"))+mutedStyle.Render(" · "+T("report.away.hint")))
	if len(m.r.AwayHits) == 0 {
		out = append(out, mutedStyle.Render("  "+T("report.away.none")))
	}
	for _, h := range m.r.AwayHits {
		hard := mutedStyle.Render(T("report.away.missed", "pct", pct(h.Difficulty*100)))
		if h.Strong {
			hard = accentStyle.Render(T("report.away.hard", "pct", pct(h.Difficulty*100)))
		}
		out = append(out, fmt.Sprintf("  %-26s %-6s %s", cut(h.Name, 26), h.QuestionID,
			T("report.away.line", "signal", signalName(h.Signal), "away", seconds(h.AwayMs), "after", seconds(h.AfterMs), "difficulty", hard)))
	}
	return out
}

func (m *Report) history() []string {
	out := []string{headStyle.Render(fmt.Sprintf("  %-20s %-8s %8s %11s %7s %8s", T("report.history.sessions"), T("report.history.code"), T("report.history.students"), T("report.history.submitted"), T("report.history.mean"), T("report.history.median")))}
	for _, h := range m.r.History {
		line := fmt.Sprintf("%-20s %-8s %8d %11d %7s %8s", day(h.StartedAt), h.Code, h.Students, h.Submitted, pct(h.Mean), pct(h.Median))
		if h.Current {
			out = append(out, selStyle.Render("▸ "+line)+mutedStyle.Render("  "+T("report.history.this")))
		} else {
			out = append(out, "  "+line)
		}
	}
	out = append(out, "", headStyle.Render("  "+T("report.leaks.title"))+mutedStyle.Render(" · "+T("report.leaks.hint")))
	switch {
	case len(m.r.History) < 2:
		out = append(out, mutedStyle.Render("  "+T("report.leaks.need_more")))
	case len(m.r.Leaks) == 0:
		out = append(out, mutedStyle.Render("  "+T("report.leaks.none")))
	}
	for _, l := range m.r.Leaks {
		out = append(out, fmt.Sprintf("  %-8s ", l.QuestionID)+T("report.leaks.line", "before", pct(l.Before*100), "before_n", l.BeforeN,
			"now", accentStyle.Render(pct(l.Now*100)), "now_n", l.NowN),
			mutedStyle.Render("           "+l.Prompt))
	}
	return out
}

// ---------------- charts ----------------

// histChart draws bins as vertical bars with the count above each one.
func histChart(bins []report.Bin, label func(report.Bin) string) []string {
	const bw, gap, h = 6, 1, 8
	data := make([]barchart.BarData, len(bins))
	top := 1
	counts := make([]string, len(bins))
	for i, b := range bins {
		data[i] = barchart.BarData{Label: label(b), Values: []barchart.BarValue{{Name: "n", Value: float64(b.Count), Style: barStyle}}}
		top = max(top, b.Count)
		if b.Count > 0 {
			counts[i] = lipgloss.PlaceHorizontal(bw, lipgloss.Center, strconv.Itoa(b.Count))
		} else {
			counts[i] = strings.Repeat(" ", bw)
		}
	}
	ch := barchart.New(len(bins)*(bw+gap), h, barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(bw), barchart.WithBarGap(gap),
		barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(float64(top)), barchart.WithStyles(mutedStyle, mutedStyle), barchart.WithDataSet(data))
	ch.Draw()
	out := []string{"  " + strings.Join(counts, strings.Repeat(" ", gap))}
	for l := range strings.SplitSeq(ch.View(), "\n") {
		out = append(out, "  "+l)
	}
	return out
}

// timelineChart stacks warnings and serious signals per slot, with a
// minute scale underneath.
func timelineChart(inc report.Incidents, w int) []string {
	n := len(inc.Timeline)
	gap := 1
	bw := max(1, min(4, (w-4)/max(1, n)-gap))
	data := make([]barchart.BarData, n)
	top := 1
	for i, t := range inc.Timeline {
		data[i] = barchart.BarData{Values: []barchart.BarValue{
			{Name: "warn", Value: float64(t.Warn), Style: warnStyle},
			{Name: "high", Value: float64(t.High), Style: barStyle},
		}}
		top = max(top, t.Warn+t.High)
	}
	ch := barchart.New(n*(bw+gap), 7, barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(bw), barchart.WithBarGap(gap),
		barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(float64(top)), barchart.WithStyles(mutedStyle, mutedStyle), barchart.WithDataSet(data))
	ch.Draw()
	lines := strings.Split(ch.View(), "\n")
	out := []string{mutedStyle.Render("  " + T("report.timeline.max", "n", top))}
	for _, l := range lines[:len(lines)-1] { // the last row holds empty labels
		out = append(out, "  "+l)
	}
	// Minute marks every few bars, where they fit.
	scale := []rune(strings.Repeat(" ", n*(bw+gap)+4))
	every := max(1, int(math.Ceil(6/float64(bw+gap))))
	for i := 0; i < n; i += every {
		mark := []rune(strconv.Itoa(inc.Timeline[i].Minute) + "'")
		at := i * (bw + gap)
		if at+len(mark) <= len(scale) {
			copy(scale[at:], mark)
		}
	}
	return append(out, mutedStyle.Render("  "+strings.TrimRight(string(scale), " ")))
}

// ---------------- formatting ----------------

func day(t time.Time) string {
	if t.IsZero() {
		return T("report.not_started")
	}
	t = t.Local()
	months := strings.Fields(T("common.months"))
	month := strconv.Itoa(int(t.Month()))
	if len(months) == 12 {
		month = months[t.Month()-1]
	}
	return T("common.date_time", "day", t.Day(), "month", month, "year", t.Year(), "time", fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute()))
}

func span(from, to time.Time) string {
	if from.IsZero() {
		return T("report.not_started")
	}
	s := day(from)
	if !to.IsZero() {
		to = to.Local()
		s += fmt.Sprintf("–%02d:%02d", to.Hour(), to.Minute())
	}
	return s
}

func seconds(ms int64) string {
	if ms >= 60_000 {
		return clock(time.Duration(ms) * time.Millisecond)
	}
	return fmt.Sprintf("%d s", (ms+500)/1000)
}

// odds shows a small probability as "1 in N".
func odds(p float64) string {
	if p <= 0 {
		return T("report.odds.nil")
	}
	n := 1 / p
	if n < 2 {
		return T("report.odds.high")
	}
	if n >= 1e6 {
		return T("report.odds.million")
	}
	s := strconv.Itoa(int(math.Round(n)))
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + T("common.thousands") + s[i:]
	}
	return T("report.odds.one_in", "n", s)
}
