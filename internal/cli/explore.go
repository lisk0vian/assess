package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/daemon"
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

// explore runs Explore until the teacher leaves, doing what they pick in
// between: the same commands as the CLI, after a form.
func explore(cmd *cobra.Command, st tui.ExploreState) error {
	dir, err := dataDir()
	if err != nil {
		return err
	}
	for {
		data, err := exploreData(cmd, dir)
		if err != nil {
			return err
		}
		action, next, err := tui.RunExplore(data, st)
		if err != nil || action == tui.ActQuit {
			return err
		}
		st = next
		var actErr error
		switch action {
		case tui.ActOpen:
			st, actErr = exploreOpen(cmd, dir, st)
		case tui.ActNew:
			actErr = exploreNew(cmd, st.Tab)
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

// errBack returns to Explore without pausing.
var errBack = errors.New("back")

func pause(cmd *cobra.Command) {
	fmt.Fprint(cmd.OutOrStdout(), "\n"+T("explore.back"))
	_, _ = readLine(cmd)
}

// exploreData reads the vault for Explore.
func exploreData(cmd *cobra.Command, dir string) (*tui.ExploreData, error) {
	d := &tui.ExploreData{}
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
			detail := T("find.exam_detail", "n", e.Sessions)
			if !e.LastUsed.IsZero() {
				detail += " · " + dateOrEmpty(e.LastUsed)
			}
			d.Tabs[tui.TabExams] = append(d.Tabs[tui.TabExams], tui.ExploreRow{ID: e.ID, Key: e.ID, Label: e.Title, Detail: detail})
		}
		classSessions := map[string]int{}
		for _, x := range sessions {
			classSessions[x.ClassID]++
		}
		for _, c := range p.Classes {
			d.Tabs[tui.TabClasses] = append(d.Tabs[tui.TabClasses], tui.ExploreRow{ID: c.ID, Key: c.Name,
				Label: T("people.n_students", "n", len(p.Members[c.ID])), Detail: T("find.exam_detail", "n", classSessions[c.ID])})
		}
		for _, stu := range p.List {
			cl := strings.Join(p.ClassNames(stu.ID), ", ")
			d.Tabs[tui.TabStudents] = append(d.Tabs[tui.TabStudents], tui.ExploreRow{ID: stu.ID, Key: stu.Ref, Label: stu.Name,
				Detail: cl, Search: []string{stu.Note}})
		}
		for i := len(sessions) - 1; i >= 0; i-- {
			x := sessions[i]
			it := sessionItem(x)
			detail := it.Detail
			if x.Submitted > 0 {
				detail += " · " + T("find.average", "pct", strconv.FormatFloat(round1(x.Average), 'f', -1, 64))
			}
			if x.State != session.StateEnded {
				detail += " · " + T("state."+string(x.State))
			}
			d.Tabs[tui.TabSessions] = append(d.Tabs[tui.TabSessions], tui.ExploreRow{ID: x.ID, Key: x.Code, Label: x.ExamTitle,
				Detail: detail, Search: []string{x.ExamID, x.ClassName}})
		}
		d.Details = func(tab int, id string) []string { return exploreDetails(cmd, dir, p, sessions, tab, id) }
		return nil
	})
	return d, err
}

// exploreDetails renders the side panel of a row.
func exploreDetails(cmd *cobra.Command, dir string, p *session.People, sessions []*session.Overview, tab int, id string) []string {
	var out []string
	switch tab {
	case tui.TabStudents:
		st := p.ByID[id]
		if st == nil {
			return nil
		}
		out = append(out, st.Name, st.Ref)
		if cl := p.ClassNames(st.ID); len(cl) > 0 {
			out = append(out, T("people.student.classes")+" "+strings.Join(cl, ", "))
		}
		if st.Note != "" {
			out = append(out, T("people.student.note")+" "+st.Note)
		}
		out = append(out, "")
		_ = withStore(cmd.Context(), dir, func(s *store) error {
			h, err := s.svc.History(cmd.Context(), st.Ref)
			if err != nil {
				return err
			}
			if len(h) == 0 {
				out = append(out, T("people.history.none"))
			}
			for _, r := range historyRows(h) {
				pct := T("astate." + r.State)
				if r.Percent != nil {
					pct = strconv.FormatFloat(*r.Percent, 'f', -1, 64) + " %"
				}
				line := r.Date + "  " + r.ExamTitle + "  " + pct
				if r.Strikes > 0 {
					line += "  ⚑" + strconv.Itoa(r.Strikes)
				}
				out = append(out, line)
			}
			return nil
		})
	case tui.TabClasses:
		for _, c := range p.Classes {
			if c.ID != id {
				continue
			}
			out = append(out, c.Name, T("people.n_students", "n", len(p.Members[c.ID])), "")
			for _, sid := range p.Members[c.ID] {
				out = append(out, p.ByID[sid].Ref+"  "+p.ByID[sid].Name)
			}
		}
	case tui.TabExams:
		out = append(out, id, "")
		for i := len(sessions) - 1; i >= 0; i-- {
			if x := sessions[i]; x.ExamID == id {
				line := x.CreatedAt.Local().Format("2006-01-02") + "  " + x.Code
				if x.ClassName != "" {
					line += "  " + x.ClassName
				}
				if x.Submitted > 0 {
					line += "  " + T("find.average", "pct", strconv.FormatFloat(round1(x.Average), 'f', -1, 64))
				}
				out = append(out, line)
			}
		}
		if len(out) == 2 {
			out = append(out, T("explore.never_given"))
		}
	case tui.TabSessions:
		for _, x := range sessions {
			if x.ID != id {
				continue
			}
			out = append(out, x.ExamTitle, x.CreatedAt.Local().Format("2006-01-02 15:04")+"  "+T("cli.start.code")+" "+x.Code)
			if x.ClassName != "" {
				out = append(out, T("archive.class")+": "+x.ClassName)
			}
			out = append(out, T("state."+string(x.State)), T("explore.session_counts", "joined", x.Joined, "submitted", x.Submitted))
			if x.Submitted > 0 {
				out = append(out, T("find.average", "pct", strconv.FormatFloat(round1(x.Average), 'f', -1, 64)))
			}
			if x.ArchivedAs != "" {
				out = append(out, "", T("explore.archived"), x.ArchivedAs)
			}
		}
	}
	return out
}

// runSub runs one of ax's commands with args, as if typed.
func runSub(cmd *cobra.Command, sub *cobra.Command, args ...string) error {
	sub.SetArgs(args)
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

// exploreOpen offers the actions of the selected row.
func exploreOpen(cmd *cobra.Command, dir string, st tui.ExploreState) (tui.ExploreState, error) {
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
		act, err := choice(stu.Ref+"  "+stu.Name, opt("history", "history"), opt("edit", "edit"), opt("rename", "rename"),
			opt("enroll", "enroll"), opt("unenroll", "unenroll"), opt("merge", "merge"), opt("forget", "forget"))
		if err != nil {
			return st, err
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
		act, err := choice(c.Name, opt("members", "members"), opt("import", "import"), opt("add_student", "add"),
			opt("gradebook", "gradebook"), opt("export", "export"), opt("rename_class", "rename"), opt("delete", "delete"))
		if err != nil {
			return st, err
		}
		if act == "members" {
			return tui.ExploreState{Tab: tui.TabStudents, Filter: c.Name}, errBack
		}
		return st, classAction(cmd, c, act)
	case tui.TabExams:
		act, err := choice(st.ID, opt("start", "start"), opt("edit_exam", "edit"), opt("sessions", "sessions"), opt("export_exam", "export"))
		if err != nil {
			return st, err
		}
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
		opts := []huh.Option[string]{opt("results", "results")}
		if x.State == session.StateEnded {
			opts = append([]huh.Option[string]{opt("report", "report")}, opts...)
		}
		act, err := choice(x.ExamTitle+"  "+x.Code, opts...)
		if err != nil {
			return st, err
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
	case "gradebook":
		file, err := askText(T("explore.ask.gradebook_file"), T("explore.ask.gradebook_hint"), vaultSafe(c.Name)+".xlsx", notEmpty)
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
		if h, _, _ := daemon.Check(dir); h == daemon.Running {
			return st, errors.New(T("cli.resume.already_running"))
		}
		sf := defaultServeFlags()
		var p *session.People
		_ = withService(cmd.Context(), dir, func(svc *session.Service) error {
			var err error
			p, err = svc.People(cmd.Context())
			return err
		})
		if p != nil && len(p.Classes) > 0 {
			opts := append([]huh.Option[string]{huh.NewOption(T("explore.no_class"), "")}, classOptions(p)...)
			if err := runForm(huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().Title(T("explore.ask.start_class")).Options(opts...).Value(&sf.class),
			))); err != nil {
				return st, err
			}
			if sf.class != "" {
				if err := runForm(huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(T("explore.ask.roster_only")).
					Description(T("vault.start.flag_roster_only")).Value(&sf.rosterOnly)))); err != nil {
					return st, err
				}
			}
		}
		return st, startExam(cmd, sf, st.ID, 0, false)
	case "edit":
		return st, editVaultExam(cmd, dir, st.ID)
	case "sessions":
		return tui.ExploreState{Tab: tui.TabSessions, Filter: st.ID}, errBack
	case "export":
		file, err := askText(T("explore.ask.export_exam"), T("vault.export.long"), st.ID+".examx", notEmpty)
		if err != nil {
			return st, err
		}
		return st, runSub(cmd, newVaultExportCmd(), st.ID, "--out", file)
	}
	return st, errBack
}

// exploreNew adds something to the current tab.
func exploreNew(cmd *cobra.Command, tab int) error {
	switch tab {
	case tui.TabExams:
		folder, err := askText(T("explore.ask.exam_folder"), T("vault.add.long"), "", fileExists)
		if err != nil {
			return err
		}
		return runSub(cmd, newVaultAddCmd(), folder)
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
