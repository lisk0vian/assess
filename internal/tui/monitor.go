// Package tui holds the teacher's terminal panels.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mdp/qrterminal/v3"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/session"
)

// Views of the panel.
const (
	viewStudents = iota
	viewRanking
	viewIncidents
	viewLink
	viewCount
)

var viewKeys = [viewCount]string{"monitor.view.students", "monitor.view.ranking", "monitor.view.incidents", "monitor.view.link"}

type mode int

const (
	modeNormal mode = iota
	modePrompt
	modeConfirm
	modeLocked
)

type promptKind int

const (
	promptMinutes promptKind = iota
	promptMessage
	promptAddID
	promptAddName
	promptRevert
	promptMessageAll
)

// Options configures the panel.
type Options struct {
	// CheckPassword verifies the panel password; nil means no password.
	CheckPassword func(string) bool
	// LockAfter locks an idle panel (only with a password). Zero disables it.
	LockAfter time.Duration
	// OnLock runs when the panel locks: it also locks the vault.
	OnLock func()
}

// Monitor is the live session panel. Quitting only detaches; the exam keeps
// running in the background.
type Monitor struct {
	client *daemon.Client
	opts   Options
	board  *daemon.Board
	offset time.Duration
	err    error
	notice string
	qr     string
	width  int
	height int
	bottom int // rows the key bar and the notice line take
	cols   int // tiles per row in the students view
	filter int // which students the view shows

	showCard bool
	showHelp bool

	view, sel int
	viewSet   bool
	mode      mode
	prompt    promptKind
	input     textinput.Model
	pendingID string  // student id typed in the first step of "add"
	reverting []int64 // penalties a revert prompt applies to
	question  string
	onYes     func() tea.Cmd
	altLabel  string // a third answer in a confirmation, e.g. "end and stop the server"
	onAlt     func() tea.Cmd
	lastInput time.Time
	report    bool     // quit to open the report of the ended session
	urls      []string // every address students can use, the likeliest first
	urlIdx    int      // the one shown with its QR
	code      string
}

// WantsReport reports whether the panel closed to open the session report.
func (m *Monitor) WantsReport() bool { return m.report }

// NewMonitor builds the panel for a connected daemon.
func NewMonitor(c *daemon.Client, st *daemon.State, opts Options) *Monitor {
	in := textinput.New()
	in.CharLimit = 300
	in.SetWidth(50)
	m := &Monitor{client: c, opts: opts, width: 120, height: 36, cols: 1, input: in, lastInput: time.Now(),
		urls: append([]string{st.StudentURL}, st.OtherURLs...), code: st.Code}
	m.makeQR()
	return m
}

// makeQR draws the QR of the selected address. It carries the code too, so
// scanning it pre-fills the join form.
func (m *Monitor) makeQR() {
	var b strings.Builder
	qrterminal.GenerateWithConfig(m.url()+"?c="+m.code, qrterminal.Config{
		Level: qrterminal.L, Writer: &b, HalfBlocks: true, QuietZone: 1,
		BlackChar: qrterminal.BLACK_BLACK, BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteChar: qrterminal.WHITE_WHITE, WhiteBlackChar: qrterminal.WHITE_BLACK,
	})
	m.qr = strings.TrimRight(b.String(), "\n")
}

// url is the address shown to students.
func (m *Monitor) url() string { return m.urls[m.urlIdx] }

type tickMsg time.Time

// stoppedMsg: the server was shut down from the panel; the panel closes.
type stoppedMsg struct{}

type boardMsg struct {
	board  *daemon.Board
	err    error
	notice string
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }

// call runs a control action off the UI goroutine.
func (m *Monitor) call(notice string, fn func(context.Context) (*daemon.Board, error)) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		b, err := fn(ctx)
		if err != nil {
			notice = ""
		}
		return boardMsg{board: b, err: err, notice: notice}
	}
}

func (m *Monitor) refresh() tea.Cmd { return m.call("", m.client.Board) }

// Init implements tea.Model.
func (m *Monitor) Init() tea.Cmd { return tea.Batch(m.refresh(), tick(), tea.RequestBackgroundColor) }

// Update implements tea.Model.
func (m *Monitor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		setDark(msg.IsDark())
	case tickMsg:
		if m.opts.CheckPassword != nil && m.opts.LockAfter > 0 && m.mode != modeLocked && time.Since(m.lastInput) > m.opts.LockAfter {
			m.lock()
		}
		return m, tea.Batch(m.refresh(), tick())
	case boardMsg:
		m.err = msg.err
		if msg.board != nil {
			m.board = msg.board
			m.offset = time.Duration(msg.board.Status.ServerNow-time.Now().UnixMilli()) * time.Millisecond
			if !m.viewSet {
				m.viewSet = true
				if m.board.Status.State == session.StateLobby {
					m.view = viewLink
				}
			}
			m.clampSel()
		}
		if msg.notice != "" {
			m.notice = msg.notice
		}
	case tea.KeyPressMsg:
		m.lastInput = time.Now()
		return m.key(msg)
	case stoppedMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Monitor) lock() {
	if m.opts.OnLock != nil {
		m.opts.OnLock()
	}
	m.mode = modeLocked
	m.input.Reset()
	m.input.EchoMode = textinput.EchoPassword
	m.input.Placeholder = T("monitor.password_placeholder")
	m.input.Focus()
}

func (m *Monitor) startPrompt(kind promptKind, question, placeholder string) tea.Cmd {
	m.mode, m.prompt, m.question = modePrompt, kind, question
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = placeholder
	return m.input.Focus()
}

func (m *Monitor) confirm(question string, yes func() tea.Cmd) {
	m.mode, m.question, m.onYes, m.altLabel, m.onAlt = modeConfirm, question, yes, "", nil
}

// confirmOr asks a question with a second way to say yes (key A).
func (m *Monitor) confirmOr(question string, yes func() tea.Cmd, altLabel string, alt func() tea.Cmd) {
	m.confirm(question, yes)
	m.altLabel, m.onAlt = altLabel, alt
}

// stopServer ends the exam (if still open) and shuts the server down.
func (m *Monitor) stopServer() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := m.client.Shutdown(ctx, true); err != nil {
			return boardMsg{err: err}
		}
		return stoppedMsg{}
	}
}

func (m *Monitor) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.mode {
	case modeLocked:
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		if k == "enter" {
			if m.opts.CheckPassword == nil || m.opts.CheckPassword(m.input.Value()) {
				m.mode, m.notice = modeNormal, T("monitor.unlocked")
				m.input.Blur()
			} else {
				m.notice = T("cli.attach.wrong_password")
			}
			m.input.Reset()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case modeConfirm:
		m.mode = modeNormal
		if isYes(k) {
			return m, m.onYes()
		}
		if (k == "a" || k == "A") && m.onAlt != nil {
			return m, m.onAlt()
		}
		m.notice = T("editor.cancelled")
		return m, nil
	case modePrompt:
		switch k {
		case "esc":
			m.mode, m.notice = modeNormal, T("editor.cancelled")
			return m, nil
		case "enter":
			return m, m.submitPrompt(strings.TrimSpace(m.input.Value()))
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m.normalKey(k)
}

func (m *Monitor) submitPrompt(v string) tea.Cmd {
	m.mode = modeNormal
	row := m.selected()
	switch m.prompt {
	case promptMinutes:
		mins, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
		if err != nil || mins <= 0 || mins > 600 || row == nil {
			m.notice = T("monitor.err.minutes")
			return nil
		}
		return m.call(T("monitor.extended", "n", v, "name", row.Name), func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AttemptAction(ctx, row.AttemptID, "extend", map[string]any{"minutes": mins})
		})
	case promptMessage:
		if v == "" || row == nil {
			return nil
		}
		return m.call(T("monitor.message_sent", "name", row.Name), func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AttemptAction(ctx, row.AttemptID, "message", map[string]any{"text": v})
		})
	case promptAddID:
		if v == "" {
			return nil
		}
		m.pendingID = v
		return m.startPrompt(promptAddName, T("monitor.prompt.add_name", "id", v), T("monitor.prompt.add_name_placeholder"))
	case promptAddName:
		id := m.pendingID
		return m.call(T("monitor.added", "name", v), func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AddStudent(ctx, id, v)
		})
	case promptMessageAll:
		if v == "" {
			return nil
		}
		return m.call(T("monitor.message_all_sent"), func(ctx context.Context) (*daemon.Board, error) {
			return m.client.MessageAll(ctx, v)
		})
	case promptRevert:
		ids := m.reverting
		return m.call(T("monitor.reverted"), func(ctx context.Context) (*daemon.Board, error) {
			return m.client.RevertPenalties(ctx, ids, v)
		})
	}
	return nil
}

// revert asks for a note and reverts the active penalties of an incident.
func (m *Monitor) revert(in *daemon.Incident) tea.Cmd {
	if in == nil {
		m.notice = T("monitor.nothing_to_revert")
		return nil
	}
	m.reverting = activePenalties(in)
	return m.startPrompt(promptRevert, T("monitor.prompt.revert", "signal", signalName(in.Kind), "name", in.Name), T("monitor.prompt.revert_placeholder"))
}

func activePenalties(in *daemon.Incident) []int64 {
	var ids []int64
	for _, p := range in.Penalties {
		if p.Active() {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// latestPenalized is the newest incident of attemptID that still costs something.
func (m *Monitor) latestPenalized(attemptID string) *daemon.Incident {
	if m.board == nil {
		return nil
	}
	for i := range m.board.Incidents {
		in := &m.board.Incidents[i]
		if in.AttemptID == attemptID && len(activePenalties(in)) > 0 {
			return in
		}
	}
	return nil
}

func (m *Monitor) selectedIncident() *daemon.Incident {
	if m.board == nil || m.view != viewIncidents || m.sel < 0 || m.sel >= len(m.board.Incidents) {
		return nil
	}
	return &m.board.Incidents[m.sel]
}

func (m *Monitor) normalKey(k string) (tea.Model, tea.Cmd) {
	if m.showHelp {
		if k == "?" || k == "esc" {
			m.showHelp = false
		}
		return m, nil
	}
	st := m.status()
	row := m.selected()
	attempt := func(action, notice string, body map[string]any) tea.Cmd {
		return m.call(notice, func(ctx context.Context) (*daemon.Board, error) {
			return m.client.AttemptAction(ctx, row.AttemptID, action, body)
		})
	}
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = true
	case "esc":
		m.showCard = false
	case "enter":
		if m.view == viewStudents && row != nil {
			m.showCard = true
		}
	case "f", "F":
		if m.view == viewStudents {
			m.filter, m.sel = (m.filter+1)%filterCount, 0
		}
	case "1", "2", "3", "4", "5":
		if m.view == viewStudents {
			m.filter, m.sel = int(k[0]-'1'), 0
		}
	case "tab":
		m.view = (m.view + 1) % viewCount
		m.sel, m.showCard = 0, false
	case "shift+tab":
		m.view = (m.view + viewCount - 1) % viewCount
		m.sel, m.showCard = 0, false
	case "left", "right":
		if m.view == viewStudents {
			if k == "left" {
				m.sel = max(0, m.sel-1)
			} else {
				m.sel++
				m.clampSel()
			}
		}
		if m.view == viewLink && len(m.urls) > 1 {
			step := 1
			if k == "left" {
				step = len(m.urls) - 1
			}
			m.urlIdx = (m.urlIdx + step) % len(m.urls)
			m.makeQR()
		}
	case "up", "k":
		m.sel = max(0, m.sel-m.step())
	case "down", "j":
		m.sel += m.step()
		m.clampSel()
	case "l", "L":
		if m.opts.CheckPassword == nil {
			m.notice = T("monitor.no_password")
		} else {
			m.lock()
		}
	case "s", "S":
		if st != nil && st.State == session.StateLobby {
			m.view, m.sel = viewStudents, 0
			return m, m.call(T("monitor.started"), m.startExam)
		}
	case "+":
		if st != nil && st.State == session.StateRunning {
			return m, m.call(T("monitor.extended_all"), func(ctx context.Context) (*daemon.Board, error) {
				if _, err := m.client.Extend(ctx, 5); err != nil {
					return nil, err
				}
				return m.client.Board(ctx)
			})
		}
	case "i", "I":
		if st != nil && st.State == session.StateEnded {
			m.report = true
			return m, tea.Quit
		}
	case "e", "E":
		switch {
		case st != nil && st.State != session.StateEnded:
			m.confirmOr(T("monitor.confirm_end"), func() tea.Cmd {
				return m.call(T("monitor.ended"), func(ctx context.Context) (*daemon.Board, error) {
					if _, err := m.client.End(ctx); err != nil {
						return nil, err
					}
					return m.client.Board(ctx)
				})
			}, T("monitor.end_and_stop"), m.stopServer)
		case st != nil:
			m.confirm(T("monitor.confirm_stop"), m.stopServer)
		}
	case "M", "shift+m":
		if st != nil && st.State != session.StateEnded {
			return m, m.startPrompt(promptMessageAll, T("monitor.prompt.message_all"), T("monitor.prompt.message_all_placeholder"))
		}
	case "P", "shift+p":
		if st != nil && st.State == session.StateRunning {
			if m.board.AllPaused {
				return m, m.call(T("monitor.resumed_all"), func(ctx context.Context) (*daemon.Board, error) { return m.client.PauseAll(ctx, false) })
			}
			m.confirm(T("monitor.confirm_pause_all"), func() tea.Cmd {
				return m.call(T("monitor.paused_all"), func(ctx context.Context) (*daemon.Board, error) { return m.client.PauseAll(ctx, true) })
			})
		}
	case "g", "G":
		if req := m.firstRequest(); req != nil && m.board.Status.Class != "" {
			notice := T("monitor.enrolled", "name", req.Name, "id", req.StudentID, "class", m.board.Status.Class)
			return m, m.call(notice, func(ctx context.Context) (*daemon.Board, error) {
				return m.client.EnrollRequest(ctx, req.ID)
			})
		}
	case "a", "A", "r", "R":
		if req := m.firstRequest(); req != nil {
			approve := k == "a" || k == "A"
			notice := T("monitor.rejected", "name", req.Name, "id", req.StudentID)
			if approve {
				notice = T("monitor.approved", "name", req.Name, "id", req.StudentID)
			}
			return m, m.call(notice, func(ctx context.Context) (*daemon.Board, error) {
				return m.client.DecideRequest(ctx, req.ID, approve)
			})
		}
	case "n", "N":
		return m, m.startPrompt(promptAddID, T("monitor.prompt.add_id"), "A-123")
	case "z", "Z":
		switch {
		case m.view == viewIncidents:
			in := m.selectedIncident()
			if in != nil && len(activePenalties(in)) == 0 {
				in = nil
			}
			return m, m.revert(in)
		case m.view == viewStudents && row != nil && row.AttemptID != "":
			return m, m.revert(m.latestPenalized(row.AttemptID))
		}
	}
	if row == nil || m.view != viewStudents {
		return m, nil
	}
	switch k {
	case "d", "D":
		label := T("monitor.confirm_remove", "name", row.Name)
		if row.AttemptID != "" {
			label = T("monitor.confirm_withdraw", "name", row.Name)
		}
		m.confirm(label, func() tea.Cmd {
			return m.call(T("monitor.removed", "name", row.Name), func(ctx context.Context) (*daemon.Board, error) {
				return m.client.RemoveParticipant(ctx, row.ParticipantID)
			})
		})
		return m, nil
	}
	if row.AttemptID == "" {
		return m, nil
	}
	switch k {
	case "t", "T":
		return m, m.startPrompt(promptMinutes, T("monitor.prompt.minutes", "name", row.Name), "5")
	case "m":
		return m, m.startPrompt(promptMessage, T("monitor.prompt.message", "name", row.Name), T("monitor.prompt.message_placeholder"))
	case "p":
		if row.Paused {
			return m, attempt("pause", T("monitor.resumed", "name", row.Name), map[string]any{"on": false})
		}
		return m, attempt("pause", T("monitor.paused", "name", row.Name), map[string]any{"on": true})
	case "u", "U":
		return m, attempt("readmit", T("monitor.readmitted", "name", row.Name), nil)
	case "v", "V":
		if row.State == session.AttemptActive && m.board.Lens {
			notice := T("monitor.lens_off", "name", row.Name)
			if row.NoLens {
				notice = T("monitor.lens_on", "name", row.Name)
			}
			return m, attempt("reading", notice, map[string]any{"no_lens": !row.NoLens})
		}
	case "x", "X":
		m.confirm(T("monitor.confirm_annul", "name", row.Name), func() tea.Cmd {
			return attempt("annul", T("monitor.annulled", "name", row.Name), nil)
		})
	}
	return m, nil
}

func (m *Monitor) startExam(ctx context.Context) (*daemon.Board, error) {
	if _, err := m.client.Start(ctx); err != nil {
		return nil, err
	}
	return m.client.Board(ctx)
}

func (m *Monitor) status() *daemon.StatusView {
	if m.board == nil {
		return nil
	}
	return &m.board.Status
}

func (m *Monitor) firstRequest() *daemonRequest {
	if m.board == nil || len(m.board.Requests) == 0 {
		return nil
	}
	r := m.board.Requests[len(m.board.Requests)-1] // oldest first
	return &daemonRequest{ID: r.ID, Name: r.Name, StudentID: r.StudentID, Kind: string(r.Kind)}
}

type daemonRequest struct{ ID, Name, StudentID, Kind string }

func (m *Monitor) rows() []daemon.StudentRow {
	if m.board == nil {
		return nil
	}
	if m.view == viewStudents {
		return m.classRows()
	}
	if m.view != viewRanking {
		return m.board.Students
	}
	var done []daemon.StudentRow
	for _, r := range m.board.Students {
		if r.State == session.AttemptSubmitted && r.Score != nil {
			done = append(done, r)
		}
	}
	sort.SliceStable(done, func(i, j int) bool {
		if *done[i].Score != *done[j].Score {
			return *done[i].Score > *done[j].Score
		}
		return done[i].UsedMs < done[j].UsedMs
	})
	return done
}

func (m *Monitor) selected() *daemon.StudentRow {
	rows := m.rows()
	if m.sel < 0 || m.sel >= len(rows) {
		return nil
	}
	r := rows[m.sel]
	return &r
}

// step is how far ↑↓ move: a row of tiles, or a line of a list.
func (m *Monitor) step() int {
	if m.view == viewStudents {
		return max(1, m.cols)
	}
	return 1
}

func (m *Monitor) clampSel() {
	n := len(m.rows())
	if m.view == viewIncidents && m.board != nil {
		n = len(m.board.Incidents)
	}
	if m.sel >= n {
		m.sel = max(n-1, 0)
	}
}

func incidentLine(in daemon.Incident) string {
	what := signalName(in.Kind)
	if in.DurationMs > 0 {
		what += fmt.Sprintf(" %ds", in.DurationMs/1000)
	}
	q := "—"
	if in.Question > 0 {
		q = strconv.Itoa(in.Question)
	}
	return pad(time.UnixMilli(in.At).Format("15:04:05"), 8) + " " + pad(in.Name, 22) + " " + pad(what, 28) + " " + pad(q, 6) + " " + penaltyLine(in)
}

func score(r daemon.StudentRow) string {
	if r.Score == nil {
		return "—"
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return f(*r.Score) + "/" + f(r.MaxScore)
}

func cut(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	h, mnt, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, mnt, s)
	}
	return fmt.Sprintf("%02d:%02d", mnt, s)
}

// noOneYet: two minutes in, no device has joined. The network may be
// keeping students out (Windows firewall, client isolation).
func (m *Monitor) noOneYet(st *daemon.StatusView) bool {
	return st != nil && st.Joined == 0 && st.UptimeMs > (2*time.Minute).Milliseconds()
}
