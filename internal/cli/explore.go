package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/backup"
	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
)

func newExploreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explore",
		Short: T("explore.short"),
		Long:  T("explore.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isTerminal() {
				return errors.New(T("cli.need_terminal"))
			}
			return explore(cmd, tui.ExploreState{Tab: tui.TabStudents})
		},
	}
}

// explore runs the hub until the teacher leaves, doing what they pick in
// between: the same commands as the CLI, after a form.
func explore(cmd *cobra.Command, st tui.ExploreState) error {
	dir, err := dataDir()
	if err != nil {
		return err
	}
	for {
		data, err := exploreData(cmd, dir)
		if err != nil {
			if st.Tab != tui.TabHome {
				return err
			}
			data = &tui.ExploreData{Notice: err.Error()} // the home screen works without a vault
		}
		hs := readHomeState(cmd, dir)
		homeRows(data, hs)
		action, next, err := tui.RunExplore(data, st)
		if err != nil || action == tui.ActQuit {
			return err
		}
		act := next.Act
		next.Act = ""
		st = next
		var actErr error
		switch {
		case action == tui.ActNew:
			actErr = exploreNew(cmd, st.Tab)
		case st.Tab == tui.TabHome:
			var leave bool
			if leave, actErr = homeAction(cmd, dir, act, hs); leave {
				return actErr
			}
		case st.Tab == tui.TabVault:
			actErr = vaultAction(cmd, dir, act)
		default:
			st, actErr = exploreOpen(cmd, dir, st, act)
		}
		if errors.Is(actErr, huh.ErrUserAborted) || errors.Is(actErr, errBack) {
			continue
		}
		if actErr != nil && !errors.Is(actErr, errSilent) {
			fmt.Fprintln(cmd.ErrOrStderr(), T("cli.error_prefix"), actErr)
		}
		pause(cmd)
	}
}

// errBack returns to the hub without pausing.
var errBack = errors.New("back")

func pause(cmd *cobra.Command) {
	fmt.Fprint(cmd.OutOrStdout(), "\n"+T("explore.back"))
	_, _ = readLine(cmd)
}

func mean(x *session.Overview) *float64 {
	if x.Submitted == 0 {
		return nil
	}
	v := x.Average
	return &v
}

// exploreData reads the vault for the hub.
func exploreData(cmd *cobra.Command, dir string) (*tui.ExploreData, error) {
	d := &tui.ExploreData{}
	var vaultDetail *tui.Detail
	err := withStore(cmd.Context(), dir, func(s *store) error {
		ctx := cmd.Context()
		d.Vault = s.vault.Meta.Name + " · " + vaultState(ctx, dir, s.vault)
		p, err := s.svc.People(ctx)
		if err != nil {
			return err
		}
		sessions, err := s.svc.Sessions(ctx)
		if err != nil {
			return err
		}
		exams, err := vaultExams(s.vault)
		if err != nil {
			return err
		}
		withUsage(exams, sessions)
		for _, e := range exams {
			row := tui.ExploreRow{ID: e.ID, Key: e.ID, Label: e.Title, Cells: []string{strconv.Itoa(e.Sessions), dateOrEmpty(e.LastUsed)}}
			for _, x := range sessions {
				if x.ExamID == e.ID && mean(x) != nil {
					row.Mean = mean(x) // sessions come oldest first: this ends on the latest
				}
			}
			d.Tabs[tui.TabExams] = append(d.Tabs[tui.TabExams], row)
		}
		classMean := map[string][]float64{}
		for _, x := range sessions {
			if v := mean(x); v != nil {
				classMean[x.ClassID] = append(classMean[x.ClassID], *v)
			}
		}
		for _, c := range p.Classes {
			label := T("people.n_students", "n", len(p.Members[c.ID]))
			if ms := classMean[c.ID]; len(ms) > 0 {
				sum := 0.0
				for _, v := range ms {
					sum += v
				}
				label += " · " + strconv.FormatFloat(round1(sum/float64(len(ms))), 'f', -1, 64) + " %"
			}
			d.Tabs[tui.TabClasses] = append(d.Tabs[tui.TabClasses], tui.ExploreRow{ID: c.ID, Key: c.Name, Label: label})
		}
		for _, stu := range p.List {
			cl := strings.Join(p.ClassNames(stu.ID), ", ")
			d.Tabs[tui.TabStudents] = append(d.Tabs[tui.TabStudents], tui.ExploreRow{ID: stu.ID, Key: stu.Ref, Label: stu.Name,
				Detail: cl, Search: []string{stu.Note}})
		}
		for i := len(sessions) - 1; i >= 0; i-- {
			x := sessions[i]
			row := tui.ExploreRow{ID: x.ID, Key: x.CreatedAt.Local().Format("2006-01-02"), Label: x.ExamTitle, Mean: mean(x),
				Cells:  []string{x.ClassName, x.Code, strconv.Itoa(x.Submitted) + "/" + strconv.Itoa(x.Joined)},
				Search: []string{x.ExamID, x.ClassName, x.Code}}
			if x.State == session.StateEnded {
				row.Direct = "report"
			}
			d.Tabs[tui.TabSessions] = append(d.Tabs[tui.TabSessions], row)
		}
		vaultDetail = readVaultDetail(ctx, dir, s, len(exams), len(p.Classes), len(p.List))
		d.Tabs[tui.TabVault] = vaultRows(s.vault.Encrypted())
		d.Details = func(tab int, id string) *tui.Detail {
			if tab == tui.TabVault {
				return vaultDetail
			}
			return exploreDetails(cmd, dir, p, sessions, tab, id)
		}
		d.Actions = func(tab int, id string) []tui.MenuItem { return exploreActions(tab, id, sessions) }
		d.Book = func(id string) (*tui.Book, error) { return readBook(cmd, dir, p, id) }
		return nil
	})
	return d, err
}

// homeRows fills the first section with what can be done now.
func homeRows(d *tui.ExploreData, st homeState) {
	d.Status = T("home.idle")
	switch {
	case st.health == daemon.Crashed:
		d.Status, d.Alert = T("hub.needs_you"), T("home.crashed")
	case st.open != nil:
		d.Status = T("home.open")
	}
	d.Tabs[tui.TabHome] = nil
	for _, o := range homeOptions(st) {
		d.Tabs[tui.TabHome] = append(d.Tabs[tui.TabHome], tui.ExploreRow{ID: o.Value, Label: o.Key, Direct: o.Value})
		if o.Value == actNew && len(d.Tabs[tui.TabClasses]) == 0 { // a first group is the other way to begin
			d.Tabs[tui.TabHome] = append(d.Tabs[tui.TabHome], tui.ExploreRow{ID: actClass, Label: T("hub.first_class"), Direct: actClass})
		}
	}
}

func item(key, label string) tui.MenuItem {
	return tui.MenuItem{Key: key, Label: T("explore.act." + label)}
}

// exploreActions lists what can be done with a row.
func exploreActions(tab int, id string, sessions []*session.Overview) []tui.MenuItem {
	switch tab {
	case tui.TabStudents:
		return []tui.MenuItem{item("history", "history"), item("edit", "edit"), item("rename", "rename"), item("enroll", "enroll"),
			item("unenroll", "unenroll"), item("merge", "merge"), item("forget", "forget")}
	case tui.TabClasses:
		return []tui.MenuItem{item("members", "members"), item("book", "gradebook"), item("import", "import"), item("add", "add_student"),
			item("export", "export"), item("rename", "rename_class"), item("delete", "delete")}
	case tui.TabExams:
		return []tui.MenuItem{item("start", "start"), item("edit", "edit_exam"), item("duplicate", "duplicate"), item("sessions", "sessions"), item("export", "export_exam")}
	case tui.TabSessions:
		for _, x := range sessions {
			if x.ID == id && x.State == session.StateEnded {
				return []tui.MenuItem{item("report", "report"), item("results", "results")}
			}
		}
		return []tui.MenuItem{item("results", "results")}
	}
	return nil
}

// exploreDetails builds the panel of a row.
func exploreDetails(cmd *cobra.Command, dir string, p *session.People, sessions []*session.Overview, tab int, id string) *tui.Detail {
	lines := func(pick func(*session.Overview) bool, limit int) []tui.DetailLine {
		var out []tui.DetailLine
		for i := len(sessions) - 1; i >= 0 && len(out) < limit; i-- {
			if x := sessions[i]; pick(x) {
				out = append(out, tui.DetailLine{Key: x.CreatedAt.Local().Format("01-02"), Text: x.ExamTitle, Percent: mean(x)})
			}
		}
		return out
	}
	switch tab {
	case tui.TabHome:
		recent := lines(func(*session.Overview) bool { return true }, 6)
		if len(recent) == 0 {
			return &tui.Detail{Title: T("hub.ready"), Sub: T("hub.ready_hint")}
		}
		return &tui.Detail{Sections: []tui.DetailSection{{Head: T("hub.recent"), Lines: recent}}}
	case tui.TabStudents:
		st := p.ByID[id]
		if st == nil {
			return nil
		}
		d := &tui.Detail{Title: st.Name, Sub: strings.Join(append([]string{st.Ref}, p.ClassNames(st.ID)...), " · ")}
		var rows []historyRow
		_ = withStore(cmd.Context(), dir, func(s *store) error {
			h, err := s.svc.History(cmd.Context(), st.Ref)
			rows = historyRows(h)
			return err
		})
		var sum float64
		n, strikes := 0, 0
		sec := tui.DetailSection{Head: T("hub.sessions_head")}
		for _, r := range rows {
			if r.Percent != nil {
				sum += *r.Percent
				n++
			}
			strikes += r.Strikes
			text := r.ExamTitle
			if r.Percent == nil {
				text += " · " + T("astate."+r.State)
			}
			sec.Lines = append(sec.Lines, tui.DetailLine{Key: r.Date[5:], Text: text, Percent: r.Percent})
		}
		avg := "—"
		if n > 0 {
			avg = strconv.FormatFloat(round1(sum/float64(n)), 'f', -1, 64) + " %"
		}
		d.Facts = [][2]string{{avg, T("hub.col.mean")}, {strconv.Itoa(len(rows)), T("hub.col.sessions")}, {strconv.Itoa(strikes), T("hub.strikes")}}
		if len(rows) == 0 {
			sec.Lines = []tui.DetailLine{{Text: T("people.history.none")}}
		}
		d.Sections = []tui.DetailSection{sec}
		if st.Note != "" {
			d.Sections = append(d.Sections, tui.DetailSection{Head: T("hub.note"), Lines: []tui.DetailLine{{Text: st.Note}}})
		}
		return d
	case tui.TabClasses:
		for _, c := range p.Classes {
			if c.ID != id {
				continue
			}
			d := &tui.Detail{Title: c.Name, Sub: T("people.n_students", "n", len(p.Members[c.ID]))}
			_ = withStore(cmd.Context(), dir, func(s *store) error {
				gb, err := buildGradebook(cmd, s.svc, c.Name)
				if err != nil {
					return err
				}
				for _, r := range gb.Students {
					d.Tiles = append(d.Tiles, tui.DetailTile{Name: r.Name, Percent: r.Average})
				}
				return nil
			})
			return d
		}
	case tui.TabExams:
		sec := tui.DetailSection{Head: T("hub.mean_by_session"), Lines: lines(func(x *session.Overview) bool { return x.ExamID == id }, 8)}
		if len(sec.Lines) == 0 {
			sec.Lines = []tui.DetailLine{{Text: T("explore.never_given")}}
		}
		for i := range sec.Lines {
			sec.Lines[i].Text = "" // the exam is the panel's title
		}
		title := id
		for _, x := range sessions {
			if x.ExamID == id {
				title = x.ExamTitle
			}
		}
		return &tui.Detail{Title: title, Sections: []tui.DetailSection{sec}}
	case tui.TabSessions:
		for _, x := range sessions {
			if x.ID == id {
				sub := T("state."+string(x.State)) + " · " + T("explore.session_counts", "joined", x.Joined, "submitted", x.Submitted)
				if x.ArchivedAs != "" {
					sub += " · " + T("explore.archived") + " " + x.ArchivedAs
				}
				return &tui.Detail{Sub: sub}
			}
		}
	}
	return nil
}

// readBook is a class's gradebook for the screen.
func readBook(cmd *cobra.Command, dir string, p *session.People, id string) (*tui.Book, error) {
	for _, c := range p.Classes {
		if c.ID != id {
			continue
		}
		b := &tui.Book{Title: c.Name}
		err := withStore(cmd.Context(), dir, func(s *store) error {
			gb, err := buildGradebook(cmd, s.svc, c.Name)
			if err != nil {
				return err
			}
			for _, e := range gb.Exams {
				b.Exams = append(b.Exams, e.Title)
			}
			for _, r := range gb.Students {
				row := tui.BookRow{Name: r.Name, Mean: r.Average}
				for _, e := range gb.Exams {
					row.Cells = append(row.Cells, r.Percent[e.ID])
				}
				b.Rows = append(b.Rows, row)
			}
			return nil
		})
		return b, err
	}
	return nil, errBack
}

func readVaultDetail(ctx context.Context, dir string, s *store, exams, classes, students int) *tui.Detail {
	d := &tui.Detail{Sub: s.vault.Dir, Facts: [][2]string{{s.vault.Meta.Name, T("hub.vault_name")}, {vaultState(ctx, dir, s.vault), T("hub.vault_state")},
		{fmt.Sprintf("%d · %d · %d", exams, classes, students), T("hub.vault_counts")}}}
	sec := tui.DetailSection{Head: T("hub.backups")}
	files, _ := backup.List(s.vault)
	for i := len(files) - 1; i >= 0 && len(sec.Lines) < 5; i-- {
		if info, err := os.Stat(files[i]); err == nil {
			sec.Lines = append(sec.Lines, tui.DetailLine{Text: info.ModTime().Format("2006-01-02 15:04") + "  " + humanSize(info.Size())})
		}
	}
	if len(sec.Lines) == 0 {
		sec.Lines = []tui.DetailLine{{Text: T("backup.none")}}
	}
	d.Sections = []tui.DetailSection{sec}
	return d
}

func vaultRows(encrypted bool) []tui.ExploreRow {
	row := func(act string) tui.ExploreRow {
		return tui.ExploreRow{ID: act, Label: T("hub.vault_act." + act), Direct: act}
	}
	rows := []tui.ExploreRow{row("backup")}
	if encrypted {
		rows = append(rows, row("passwd"), row("lock"))
	} else {
		rows = append(rows, row("encrypt"))
	}
	return append(rows, row("switch"), row("settings"))
}

// vaultAction runs an action of the Vault section.
func vaultAction(cmd *cobra.Command, dir, act string) error {
	switch act {
	case "backup":
		return runSub(cmd, newBackupCmd())
	case "passwd":
		return runSub(cmd, newVaultPasswdCmd())
	case "encrypt":
		return runSub(cmd, newVaultEncryptCmd())
	case "lock":
		return runSub(cmd, newVaultLockCmd())
	case "settings":
		if err := editSettings(cmd); err != nil {
			return err
		}
		return errBack
	case "switch":
		var opts []huh.Option[string]
		for _, e := range loadRegistry(dir).Vaults {
			opts = append(opts, huh.NewOption(e.Name+"  "+e.Path, e.Name))
		}
		if len(opts) == 0 {
			return errors.New(T("hub.no_other_vault"))
		}
		name, err := choice(T("hub.vault_act.switch"), opts...)
		if err != nil {
			return err
		}
		return runSub(cmd, newVaultUseCmd(), name)
	}
	return errBack
}

// runSub runs one of ax's commands with args, as if typed.
func runSub(cmd *cobra.Command, sub *cobra.Command, args ...string) error {
	sub.SetArgs(append([]string{}, args...)) // nil would make cobra read os.Args
	sub.SetContext(cmd.Context())
	sub.SetOut(cmd.OutOrStdout())
	sub.SetErr(cmd.ErrOrStderr())
	sub.SetIn(cmd.InOrStdin())
	sub.SilenceErrors, sub.SilenceUsage = true, true
	return sub.Execute()
}

// askText asks one line.
func askText(title, desc, value string, validate func(string) error) (string, error) {
	in := huh.NewInput().Title(title).Description(desc).Value(&value)
	if validate != nil {
		in = in.Validate(validate)
	}
	err := runForm(huh.NewForm(huh.NewGroup(in)))
	return strings.TrimSpace(value), err
}

func notEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New(T("explore.required"))
	}
	return nil
}

// choice asks which action to run.
func choice(title string, opts ...huh.Option[string]) (string, error) {
	pick := ""
	opts = append(opts, huh.NewOption(T("explore.act.back"), ""))
	err := runForm(huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(opts...).Value(&pick))))
	if err == nil && pick == "" {
		return "", errBack
	}
	return pick, err
}

func opt(key, value string) huh.Option[string] { return huh.NewOption(T("explore.act."+key), value) }

// exploreOpen runs an action on the selected row.
func exploreOpen(cmd *cobra.Command, dir string, st tui.ExploreState, act string) (tui.ExploreState, error) {
	var p *session.People
	var sessions []*session.Overview
	if err := withStore(cmd.Context(), dir, func(s *store) error {
		var err error
		if p, err = s.svc.People(cmd.Context()); err != nil {
			return err
		}
		sessions, err = s.svc.Sessions(cmd.Context())
		return err
	}); err != nil {
		return st, err
	}
	switch st.Tab {
	case tui.TabStudents:
		stu := p.ByID[st.ID]
		if stu == nil {
			return st, errBack
		}
		return st, studentAction(cmd, p, stu, act)
	case tui.TabClasses:
		var c *session.Class
		for _, x := range p.Classes {
			if x.ID == st.ID {
				c = x
			}
		}
		if c == nil {
			return st, errBack
		}
		if act == "members" {
			return tui.ExploreState{Tab: tui.TabStudents, Filter: c.Name}, errBack
		}
		return st, classAction(cmd, c, act)
	case tui.TabExams:
		return examAction(cmd, dir, st, act)
	case tui.TabSessions:
		var x *session.Overview
		for _, s := range sessions {
			if s.ID == st.ID {
				x = s
			}
		}
		if x == nil {
			return st, errBack
		}
		switch act {
		case "report":
			if err := requirePassword(cmd, dir); err != nil {
				return st, err
			}
			if err := showReport(cmd, dir, x.ID); err != nil {
				return st, err
			}
			return st, errBack
		case "results":
			file, err := askText(T("explore.ask.results_file"), "", vaultSafe(x.ExamTitle+" "+x.Code)+".csv", notEmpty)
			if err != nil {
				return st, err
			}
			return st, runSub(cmd, newResultsCmd(), "--session", x.ID, "--out", file)
		}
	}
	return st, errBack
}

func vaultSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s)
}

func classOptions(p *session.People) []huh.Option[string] {
	var out []huh.Option[string]
	for _, c := range p.Classes {
		out = append(out, huh.NewOption(c.Name, c.Name))
	}
	return out
}

func studentAction(cmd *cobra.Command, p *session.People, stu *session.Student, act string) error {
	switch act {
	case "history":
		return runSub(cmd, newHistoryCmd(), stu.Ref)
	case "edit":
		name, note := stu.Name, stu.Note
		err := runForm(huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(T("explore.ask.name")).Value(&name).Validate(notEmpty),
			huh.NewInput().Title(T("explore.ask.note")).Value(&note),
		)))
		if err != nil {
			return err
		}
		return runSub(cmd, newStudentEditCmd(), stu.Ref, "--name", name, "--note", note)
	case "rename":
		ref, err := askText(T("explore.ask.new_id"), T("people.student.rename_long"), stu.Ref, notEmpty)
		if err != nil {
			return err
		}
		return runSub(cmd, newStudentRenameCmd(), stu.Ref, ref)
	case "enroll", "unenroll":
		if len(p.Classes) == 0 {
			return errors.New(T("people.class.none"))
		}
		class := p.Classes[0].Name
		if err := runForm(huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(T("explore.ask.class")).
			Options(classOptions(p)...).Value(&class)))); err != nil {
			return err
		}
		if act == "enroll" {
			return runSub(cmd, newClassAddCmd(), class, stu.Ref)
		}
		return runSub(cmd, newClassRemoveCmd(), class, stu.Ref)
	case "merge":
		into, err := askText(T("explore.ask.merge_into"), T("people.student.merge_long"), "", func(s string) error {
			if p.ByRef[session.NormalizeStudentID(s)] == nil {
				return errors.New(T("explore.unknown_student"))
			}
			return nil
		})
		if err != nil {
			return err
		}
		return runSub(cmd, newStudentMergeCmd(), stu.Ref, into)
	case "forget":
		return runSub(cmd, newStudentForgetCmd(), stu.Ref)
	}
	return errBack
}

func classAction(cmd *cobra.Command, c *session.Class, act string) error {
	switch act {
	case "import":
		file, err := askText(T("explore.ask.csv"), T("people.class.import_long"), "", fileExists)
		if err != nil {
			return err
		}
		return runSub(cmd, newClassImportCmd(), c.Name, file)
	case "add":
		ref, name := "", ""
		err := runForm(huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(T("explore.ask.id")).Value(&ref).Validate(notEmpty),
			huh.NewInput().Title(T("explore.ask.name")).Value(&name).Validate(notEmpty),
		)))
		if err != nil {
			return err
		}
		return runSub(cmd, newStudentAddCmd(), ref, name, "--class", c.Name)
	case "gradebook", "gradebook_csv":
		ext := ".xlsx"
		if act == "gradebook_csv" {
			ext = ".csv"
		}
		file, err := askText(T("explore.ask.gradebook_file"), T("explore.ask.gradebook_hint"), vaultSafe(c.Name)+ext, notEmpty)
		if err != nil {
			return err
		}
		return runSub(cmd, newGradebookCmd(), c.Name, "--out", file)
	case "export":
		file, err := askText(T("explore.ask.export_file"), "", vaultSafe(c.Name)+".csv", notEmpty)
		if err != nil {
			return err
		}
		return runSub(cmd, newClassExportCmd(), c.Name, "--out", file)
	case "rename":
		name, err := askText(T("explore.ask.class_name"), "", c.Name, notEmpty)
		if err != nil {
			return err
		}
		return runSub(cmd, newClassRenameCmd(), c.Name, name)
	case "delete":
		return runSub(cmd, newClassDeleteCmd(), c.Name)
	}
	return errBack
}

func fileExists(s string) error {
	if _, err := os.Stat(strings.TrimSpace(s)); err != nil {
		return errors.New(T("explore.no_file"))
	}
	return nil
}

func examAction(cmd *cobra.Command, dir string, st tui.ExploreState, act string) (tui.ExploreState, error) {
	switch act {
	case "start":
		return st, prepareAndStart(cmd, dir, st.ID, defaultServeFlags())
	case "edit":
		return st, editVaultExam(cmd, dir, st.ID)
	case "sessions":
		return tui.ExploreState{Tab: tui.TabSessions, Filter: st.ID}, errBack
	case "duplicate":
		return st, duplicateExam(cmd, dir, st.ID)
	case "import":
		file, err := askText(T("explore.ask.import_exam"), T("vault.add.long"), "", fileExists)
		if err != nil {
			return st, err
		}
		return st, runSub(cmd, newVaultAddCmd(), file)
	case "export":
		how, err := choice(T("explore.ask.export_how"), opt("export_sealed", "sealed"), opt("export_no_key", "no_key"), opt("export_plain", "plain"))
		if err != nil {
			return st, err
		}
		file, err := askText(T("explore.ask.export_exam"), "", st.ID+".examx", notEmpty)
		if err != nil {
			return st, err
		}
		args := []string{st.ID, "--out", file}
		switch how {
		case "no_key":
			args = append(args, "--no-key")
		case "plain":
			args = append(args, "--no-encrypt")
		}
		return st, runSub(cmd, newVaultExportCmd(), args...)
	}
	return st, errBack
}

// duplicateExam copies an exam's questions, answers and rules under a new id.
// Sessions stay with the original.
func duplicateExam(cmd *cobra.Command, dir, id string) error {
	v, _, err := activeVault(dir)
	if err != nil {
		return err
	}
	tmp, cleanup, err := examFolder(cmd, dir, id)
	if err != nil {
		return err
	}
	defer cleanup()
	d, err := exam.OpenDraft(tmp)
	if err != nil {
		return err
	}
	title, err := askText(T("explore.ask.copy_title"), "", T("explore.copy_of", "title", d.Exam.Manifest.Title), notEmpty)
	if err != nil {
		return err
	}
	newID := id + "-2"
	for n := 3; v.Exists(examFile(newID)); n++ {
		newID = id + "-" + strconv.Itoa(n)
	}
	if err := d.Rename(newID, title); err != nil {
		return err
	}
	if _, err := storeExam(v, tmp, false); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), T("explore.copied", "title", title))
	return nil
}

// exploreNew adds something to the current tab.
func exploreNew(cmd *cobra.Command, tab int) error {
	switch tab {
	case tui.TabExams:
		return newExamWizard(cmd)
	case tui.TabClasses:
		name, file := "", ""
		err := runForm(huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(T("explore.ask.class_name")).Value(&name).Validate(notEmpty),
			huh.NewInput().Title(T("explore.ask.csv_optional")).Description(T("people.class.import_long")).Value(&file).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return nil
					}
					return fileExists(s)
				}),
		)))
		if err != nil {
			return err
		}
		if strings.TrimSpace(file) != "" {
			return runSub(cmd, newClassImportCmd(), name, strings.TrimSpace(file))
		}
		return runSub(cmd, newClassCreateCmd(), name)
	case tui.TabStudents:
		ref, name, class := "", "", ""
		fields := []huh.Field{
			huh.NewInput().Title(T("explore.ask.id")).Value(&ref).Validate(notEmpty),
			huh.NewInput().Title(T("explore.ask.name")).Value(&name).Validate(notEmpty),
		}
		dir, _ := dataDir()
		var p *session.People
		_ = withService(cmd.Context(), dir, func(svc *session.Service) error {
			var err error
			p, err = svc.People(cmd.Context())
			return err
		})
		if p != nil && len(p.Classes) > 0 {
			opts := append([]huh.Option[string]{huh.NewOption(T("explore.no_class"), "")}, classOptions(p)...)
			fields = append(fields, huh.NewSelect[string]().Title(T("explore.ask.class")).Options(opts...).Value(&class))
		}
		if err := runForm(huh.NewForm(huh.NewGroup(fields...))); err != nil {
			return err
		}
		args := []string{ref, name}
		if class != "" {
			args = append(args, "--class", class)
		}
		return runSub(cmd, newStudentAddCmd(), args...)
	}
	return errBack
}
