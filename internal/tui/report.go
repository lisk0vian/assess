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

var repNames = [repCount]string{"Resumen", "Preguntas", "Incidencias", "Indicios", "Historial"}

var (
	barStyle  = lipgloss.NewStyle().Foreground(accent)
	warnStyle = lipgloss.NewStyle().Foreground(muted)
)

// Report is the read-only panel of a finished session.
type Report struct {
	r             *report.Report
	view, scroll  int
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
	return &Report{r: r, idle: idle, lastInput: time.Now(), width: 100, height: 40}
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
			m.view, m.scroll = (m.view+1)%repCount, 0
		case "left", "h", "shift+tab":
			m.view, m.scroll = (m.view+repCount-1)%repCount, 0
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll--
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
				m.view, m.scroll = n-1, 0
			}
		}
	}
	return m, nil
}

func (m *Report) w() int { return max(72, min(m.width, 140)) }

// View draws the header, the current view scrolled to fit, and the keys.
func (m *Report) View() tea.View {
	w := m.w()
	head := []string{m.header(), m.tabs(), mutedStyle.Render(strings.Repeat("─", w))}
	body := m.body(m.view, w)
	room := max(3, m.height-len(head)-2)
	m.scroll = min(max(0, m.scroll), max(0, len(body)-room))
	end := min(len(body), m.scroll+room)

	var b strings.Builder
	for _, l := range head {
		b.WriteString(l + "\n")
	}
	for _, l := range body[m.scroll:end] {
		b.WriteString(l + "\n")
	}
	for range room - (end - m.scroll) {
		b.WriteString("\n")
	}
	k := func(key, label string) string { return keyStyle.Render(key) + " " + label }
	keys := []string{k("←→", "vista"), k("1-5", "ir a")}
	if len(body) > room {
		keys = append(keys, k("↑↓", fmt.Sprintf("desplazar (%d-%d de %d)", m.scroll+1, end, len(body))))
	}
	keys = append(keys, k("Q", "salir"))
	b.WriteString(mutedStyle.Render(strings.Repeat("─", w)) + "\n" + strings.Join(keys, "   "))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *Report) header() string {
	s := m.r.Session
	return brandStyle.Render("ASSESS") + mutedStyle.Render("  ·  informe  ·  ") + s.ExamTitle +
		mutedStyle.Render("  ·  sesión "+s.Code+"  ·  "+span(s.StartedAt, s.EndedAt))
}

func (m *Report) tabs() string {
	parts := make([]string, repCount)
	for i, n := range repNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		if i == m.view {
			parts[i] = selStyle.Render(" " + label + " ")
		} else {
			parts[i] = mutedStyle.Render(" " + label + " ")
		}
	}
	return strings.Join(parts, " ")
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
		b.WriteString("\n== " + strings.ToUpper(repNames[v]) + " ==\n")
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
		fmt.Sprintf("  %s   %s   %s   %s", count(s.Students, "alumno", "alumnos"), count(s.Submitted, "entregado", "entregados"),
			count(s.Annulled, "anulado", "anulados"), count(s.Withdrawn, "retirado", "retirados")),
		"",
	}
	if s.Submitted == 0 {
		return append(out, mutedStyle.Render("  Nadie entregó el examen: no hay notas que analizar."))
	}
	out = append(out,
		fmt.Sprintf("  Media %s   Mediana %s   Desviación %s   Mínimo %s   Máximo %s   Tiempo mediano %s",
			boldStyle.Render(pct(s.Mean)), boldStyle.Render(pct(s.Median)), num(s.StdDev), pct(s.Min), pct(s.Max), minutes(s.MedianMinutes)),
		"",
		headStyle.Render("  NOTAS")+mutedStyle.Render(" · % del máximo, exámenes entregados"),
	)
	out = append(out, histChart(m.r.Scores, func(b report.Bin) string { return fmt.Sprintf("%g-%g", b.From, b.To) })...)
	out = append(out, "", headStyle.Render("  TIEMPO EMPLEADO")+mutedStyle.Render(" · minutos desde que empezó cada alumno"))
	out = append(out, histChart(m.r.Times, func(b report.Bin) string { return fmt.Sprintf("%g-%g", b.From, b.To) })...)

	out = append(out, "", headStyle.Render("  PARA REVISAR")+mutedStyle.Render(" · alumnos con strikes o indicios"))
	n := 0
	for _, st := range m.r.Students {
		if st.Strikes == 0 && st.AwayHits == 0 && st.Pairs == 0 {
			continue
		}
		n++
		var why []string
		if st.Strikes > 0 {
			why = append(why, count(st.Strikes, "strike", "strikes"))
		}
		if st.Pairs > 0 {
			why = append(why, count(st.Pairs, "pareja parecida", "parejas parecidas"))
		}
		if st.AwayHits > 0 {
			why = append(why, fmt.Sprintf("salió y acertó %d %s", st.AwayHits, plural(st.AwayHits, "vez", "veces")))
		}
		score := "—"
		if st.Percent != nil {
			score = pct(*st.Percent)
		}
		out = append(out, fmt.Sprintf("  %-26s %-10s %6s   %s", cut(st.Name, 26), stateName(st.State), score, strings.Join(why, " · ")))
	}
	if n == 0 {
		out = append(out, mutedStyle.Render("  Nadie acumula strikes ni indicios."))
	}
	return out
}

func (m *Report) questions(w int) []string {
	qs := m.r.Questions
	out := []string{headStyle.Render("  % DE ACIERTOS POR PREGUNTA") + mutedStyle.Render(" · entre quienes la recibieron")}
	data := make([]barchart.BarData, 0, len(qs))
	labelW := len("PREG.")
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
	out = append(out, "", headStyle.Render(fmt.Sprintf("  %-*s  %-10s %8s %9s %8s   %s", labelW, "PREG.", "TIPO", "ACIERTOS", "EN BLANCO", "DISCRIM.", "ENUNCIADO")))
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
			notes = append(notes, mutedStyle.Render("nadie la recibió (pool)"))
		case q.Discrimination != nil && *q.Discrimination < 0:
			notes = append(notes, accentStyle.Render("la aciertan más quienes peor van: revisa la clave o el enunciado"))
		case q.Discrimination != nil && *q.Discrimination < 0.15 && q.Rate > 0 && q.Rate < 1:
			notes = append(notes, mutedStyle.Render("apenas distingue a quien sabe de quien no"))
		}
		if q.Leak {
			notes = append(notes, accentStyle.Render("mucho más fácil que en sesiones anteriores: quizá circula"))
		}
		if q.AwayHits > 0 {
			notes = append(notes, accentStyle.Render(fmt.Sprintf("%d %s tras salir del examen", q.AwayHits, plural(q.AwayHits, "acierto", "aciertos"))))
		}
		if q.Voided > 0 {
			notes = append(notes, mutedStyle.Render(fmt.Sprintf("anulada a %s", count(q.Voided, "alumno", "alumnos"))))
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
	out := []string{fmt.Sprintf("  %s · %s · %s activos · %s · %s restados · %s",
		count(warn, "aviso", "avisos"), count(high, "señal grave", "señales graves"), strconv.Itoa(inc.Strikes)+" strikes",
		count(inc.Reverted, "penalización revertida", "penalizaciones revertidas"), minutes(inc.TimeLostMin),
		count(inc.VoidedAnswers, "pregunta anulada", "preguntas anuladas")), ""}
	if warn+high == 0 {
		return append(out, mutedStyle.Render("  No hubo señales de integridad en esta sesión."))
	}
	out = append(out, headStyle.Render("  SEÑALES A LO LARGO DEL EXAMEN")+mutedStyle.Render(fmt.Sprintf(" · cada barra, %d min · ", inc.Step))+
		warnStyle.Render("█")+" aviso  "+barStyle.Render("█")+" grave")
	out = append(out, timelineChart(inc, w)...)
	out = append(out, "", headStyle.Render(fmt.Sprintf("  %-34s %7s %7s", "POR TIPO", "AVISOS", "GRAVES")))
	for _, k := range inc.ByKind {
		name := signalNames[k.Kind]
		if name == "" {
			name = k.Kind
		}
		out = append(out, fmt.Sprintf("  %-34s %7d %7d", name, k.Warn, k.High))
	}
	return out
}

func (m *Report) evidence() []string {
	out := []string{mutedStyle.Render("  Son indicios, no pruebas: revisa las respuestas y habla con el alumno antes de decidir."), "",
		headStyle.Render("  RESPUESTAS PARECIDAS") + mutedStyle.Render(" · mismos errores poco comunes, o guardados a la vez con distinto orden")}
	if len(m.r.Pairs) == 0 {
		out = append(out, mutedStyle.Render("  Ninguna pareja se parece más de lo que explica el azar."))
	}
	for _, p := range m.r.Pairs {
		level := mutedStyle.Render("MEDIO")
		if p.Level == report.LevelHigh {
			level = accentStyle.Render("ALTO ")
		}
		line := fmt.Sprintf("  %s  %s ↔ %s", level, boldStyle.Render(p.Students[0].Name), boldStyle.Render(p.Students[1].Name))
		var why []string
		if p.SameWrong > 0 {
			why = append(why, fmt.Sprintf("%d de %d errores en común son idénticos (por azar, ~%s; probabilidad %s)",
				p.SameWrong, p.BothWrong, num(p.Expected), odds(p.P)))
		}
		if p.Synced > 0 {
			why = append(why, fmt.Sprintf("%s a la vez, con las preguntas en distinto orden", count(p.Synced, "respuesta guardada", "respuestas guardadas")))
		}
		out = append(out, line, "         "+strings.Join(why, mutedStyle.Render(" · ")))
		if len(p.Questions) > 0 {
			out = append(out, mutedStyle.Render("         errores idénticos en: "+strings.Join(p.Questions, " ")))
		}
	}
	out = append(out, "", headStyle.Render("  SALIÓ Y ACERTÓ")+mutedStyle.Render(" · respuesta correcta guardada justo al volver a la pregunta"))
	if len(m.r.AwayHits) == 0 {
		out = append(out, mutedStyle.Render("  Nadie acertó justo después de salir del examen."))
	}
	for _, h := range m.r.AwayHits {
		hard := mutedStyle.Render(fmt.Sprintf("la falló el %s del resto", pct(h.Difficulty*100)))
		if h.Strong {
			hard = accentStyle.Render(fmt.Sprintf("difícil: la falló el %s del resto", pct(h.Difficulty*100)))
		}
		sig := signalNames[h.Signal]
		out = append(out, fmt.Sprintf("  %-26s %-6s %s %s → acertó %s después · %s", cut(h.Name, 26), h.QuestionID, sig,
			seconds(h.AwayMs), seconds(h.AfterMs), hard))
	}
	return out
}

func (m *Report) history() []string {
	out := []string{headStyle.Render(fmt.Sprintf("  %-20s %-8s %8s %11s %7s %8s", "SESIONES DEL EXAMEN", "CÓDIGO", "ALUMNOS", "ENTREGADOS", "MEDIA", "MEDIANA"))}
	for _, h := range m.r.History {
		line := fmt.Sprintf("%-20s %-8s %8d %11d %7s %8s", day(h.StartedAt), h.Code, h.Students, h.Submitted, pct(h.Mean), pct(h.Median))
		if h.Current {
			out = append(out, selStyle.Render("▸ "+line)+mutedStyle.Render("  esta sesión"))
		} else {
			out = append(out, "  "+line)
		}
	}
	out = append(out, "", headStyle.Render("  PREGUNTAS QUE QUIZÁ CIRCULAN")+mutedStyle.Render(" · aciertos muy por encima de sesiones anteriores"))
	switch {
	case len(m.r.History) < 2:
		out = append(out, mutedStyle.Render("  Hace falta otra sesión terminada de este examen para comparar."))
	case len(m.r.Leaks) == 0:
		out = append(out, mutedStyle.Render("  Ninguna pregunta se volvió mucho más fácil que antes."))
	}
	for _, l := range m.r.Leaks {
		out = append(out, fmt.Sprintf("  %-8s antes %s (%d) → ahora %s (%d) · %s", l.QuestionID, pct(l.Before*100), l.BeforeN,
			accentStyle.Render(pct(l.Now*100)), l.NowN, "cámbiala o reescríbela para la próxima"),
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
			{Name: "aviso", Value: float64(t.Warn), Style: warnStyle},
			{Name: "grave", Value: float64(t.High), Style: barStyle},
		}}
		top = max(top, t.Warn+t.High)
	}
	ch := barchart.New(n*(bw+gap), 7, barchart.WithNoAutoBarWidth(), barchart.WithBarWidth(bw), barchart.WithBarGap(gap),
		barchart.WithNoAutoMaxValue(), barchart.WithMaxValue(float64(top)), barchart.WithStyles(mutedStyle, mutedStyle), barchart.WithDataSet(data))
	ch.Draw()
	lines := strings.Split(ch.View(), "\n")
	out := []string{mutedStyle.Render(fmt.Sprintf("  máx. %d", top))}
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

var monthsES = [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

func day(t time.Time) string {
	if t.IsZero() {
		return "sin empezar"
	}
	t = t.Local()
	return fmt.Sprintf("%d %s %d %02d:%02d", t.Day(), monthsES[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}

func span(from, to time.Time) string {
	if from.IsZero() {
		return "sin empezar"
	}
	s := day(from)
	if !to.IsZero() {
		to = to.Local()
		s += fmt.Sprintf("–%02d:%02d", to.Hour(), to.Minute())
	}
	return s
}

// num formats with a decimal comma, as Spanish readers expect.
func num(v float64) string {
	s := strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	return strings.Replace(s, ".", ",", 1)
}

func pct(v float64) string { return strconv.Itoa(int(math.Round(v))) + " %" }

func minutes(v float64) string { return num(v) + " min" }

func seconds(ms int64) string {
	if ms >= 60_000 {
		return clock(time.Duration(ms) * time.Millisecond)
	}
	return fmt.Sprintf("%d s", (ms+500)/1000)
}

// odds shows a small probability as "1 entre N".
func odds(p float64) string {
	if p <= 0 {
		return "casi nula"
	}
	n := 1 / p
	if n < 2 {
		return "alta"
	}
	if n >= 1e6 {
		return "menos de 1 entre un millón"
	}
	s := strconv.Itoa(int(math.Round(n)))
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return "1 entre " + s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func count(n int, one, many string) string { return strconv.Itoa(n) + " " + plural(n, one, many) }

func typeName(t string) string {
	switch t {
	case "single":
		return "una opción"
	case "multiple":
		return "varias"
	case "truefalse":
		return "V/F"
	case "short":
		return "corta"
	}
	return t
}

func stateName(s string) string {
	switch s {
	case "submitted":
		return "entregado"
	case "annulled":
		return "anulado"
	case "withdrawn":
		return "retirado"
	case "active":
		return "sin entregar"
	}
	return s
}
