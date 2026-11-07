package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"golang.org/x/text/encoding/charmap"

	"github.com/lisk0vian/assess/internal/archive"
	"github.com/lisk0vian/assess/internal/search"
	"github.com/lisk0vian/assess/internal/session"
)

// listFormat is the --json / --csv pair of the listing commands.
type listFormat struct{ json, csv bool }

func (f *listFormat) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.json, "json", false, T("cli.flag.json"))
	cmd.Flags().BoolVar(&f.csv, "csv", false, T("people.flag.csv"))
}

// write prints rows as JSON (v), CSV (header + records) or an aligned table.
func (f *listFormat) write(w io.Writer, v any, header []string, records [][]string) error {
	switch {
	case f.json:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case f.csv:
		cw := csv.NewWriter(w)
		_ = cw.Write(header)
		for _, r := range records {
			_ = cw.Write(r)
		}
		cw.Flush()
		return cw.Error()
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range records {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}

// peopleCmd runs fn with the open store.
func peopleCmd(cmd *cobra.Command, fn func(*store) error) error {
	dir, err := dataDir()
	if err != nil {
		return err
	}
	err = withStore(cmd.Context(), dir, fn)
	if errors.Is(err, errCancelled) {
		return errSilent
	}
	return err
}

// syncClass rewrites the readable copy of a class in classes/.
func syncClass(ctx context.Context, s *store, classID string) error {
	return archive.WriteClass(ctx, s.vault, s.svc, classID)
}

func classFile(name string) string { return archive.ClassFile(name) }

// syncClassesOf rewrites the copies of every class a student is in.
func syncClassesOf(ctx context.Context, s *store, classIDs []string) {
	for _, id := range classIDs {
		_ = syncClass(ctx, s, id)
	}
}

// rewriteArchives writes again the archives of the given sessions (after a
// student was forgotten or merged).
func rewriteArchives(ctx context.Context, s *store, sessions []string) {
	seen := map[string]bool{}
	for _, id := range sessions {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := archive.Write(ctx, s.vault, s.svc, id); err != nil {
			fmt.Fprintln(os.Stderr, T("people.archive_failed", "err", err))
		}
	}
}

// ================= ax student =================

func newStudentCmd() *cobra.Command {
	var f listFormat
	cmd := &cobra.Command{
		Use:               "student [id]",
		Short:             T("people.student.short"),
		Long:              T("people.student.long"),
		Example:           T("people.student.example"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeStudents,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listStudents(cmd, "", &f)
			}
			return showStudent(cmd, args[0], &f)
		},
	}
	f.register(cmd)
	cmd.AddCommand(newStudentListCmd(), newStudentAddCmd(), newStudentEditCmd(), newStudentRenameCmd(),
		newStudentMergeCmd(), newStudentForgetCmd())
	return cmd
}

func newStudentListCmd() *cobra.Command {
	var f listFormat
	var class string
	cmd := &cobra.Command{
		Use:   "list",
		Short: T("people.student.list_short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return listStudents(cmd, class, &f) },
	}
	f.register(cmd)
	cmd.Flags().StringVar(&class, "class", "", T("people.flag.class"))
	_ = cmd.RegisterFlagCompletionFunc("class", completeClasses)
	return cmd
}

type studentRow struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Classes []string `json:"classes"`
	Note    string   `json:"note,omitempty"`
}

func listStudents(cmd *cobra.Command, class string, f *listFormat) error {
	return peopleCmd(cmd, func(s *store) error {
		p, err := s.svc.People(cmd.Context())
		if err != nil {
			return err
		}
		list := p.List
		if class != "" {
			c, err := findClass(p, class)
			if err != nil {
				return err
			}
			list = nil
			for _, id := range p.Members[c.ID] {
				list = append(list, p.ByID[id])
			}
		}
		rows := []studentRow{}
		var recs [][]string
		for _, st := range list {
			cl := p.ClassNames(st.ID)
			rows = append(rows, studentRow{ID: st.Ref, Name: st.Name, Classes: cl, Note: st.Note})
			recs = append(recs, []string{st.Ref, st.Name, strings.Join(cl, ", "), st.Note})
		}
		if len(rows) == 0 && !f.json && !f.csv {
			fmt.Fprintln(cmd.OutOrStdout(), T("people.student.none"))
			return nil
		}
		return f.write(cmd.OutOrStdout(), rows, strings.Split(T("people.student.header"), "|"), recs)
	})
}

// historyRow is one line of a student's history.
type historyRow struct {
	Date      string   `json:"date"`
	Session   string   `json:"session"`
	Code      string   `json:"code"`
	ExamID    string   `json:"exam_id"`
	ExamTitle string   `json:"exam_title"`
	Class     string   `json:"class,omitempty"`
	State     string   `json:"state"`
	Score     *float64 `json:"score"`
	MaxScore  float64  `json:"max_score"`
	Percent   *float64 `json:"percent"`
	Strikes   int      `json:"strikes"`
	Signals   int      `json:"signals"`
}

func historyRows(h []*session.Participation) []historyRow {
	rows := []historyRow{}
	for _, x := range h {
		r := historyRow{Date: x.Session.CreatedAt.Local().Format("2006-01-02"), Session: x.Session.ID, Code: x.Session.Code,
			ExamID: x.ExamID, ExamTitle: x.ExamTitle, Class: x.ClassName, State: "absent", Strikes: x.Strikes, Signals: x.Signals}
		if a := x.Attempt; a != nil {
			r.State, r.MaxScore = string(a.State), a.MaxScore
			if !a.SubmittedAt.IsZero() {
				score := a.Score
				r.Score = &score
				if a.MaxScore > 0 {
					pc := math.Round(a.Score/a.MaxScore*1000) / 10
					r.Percent = &pc
				}
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func historyRecords(rows []historyRow) [][]string {
	var recs [][]string
	for _, r := range rows {
		pct := ""
		if r.Percent != nil {
			pct = strconv.FormatFloat(*r.Percent, 'f', -1, 64)
		}
		recs = append(recs, []string{r.Date, r.ExamTitle, r.Class, T("astate." + r.State), pct, strconv.Itoa(r.Strikes), r.Code})
	}
	return recs
}

func showStudent(cmd *cobra.Command, ref string, f *listFormat) error {
	return peopleCmd(cmd, func(s *store) error {
		st, p, err := resolveStudent(cmd, s.svc, ref)
		if err != nil {
			return err
		}
		h, err := s.svc.History(cmd.Context(), st.Ref)
		if err != nil {
			return err
		}
		rows := historyRows(h)
		w := cmd.OutOrStdout()
		if f.json {
			return f.write(w, map[string]any{"id": st.Ref, "name": st.Name, "note": st.Note,
				"classes": p.ClassNames(st.ID), "history": rows}, nil, nil)
		}
		if f.csv {
			return f.write(w, nil, strings.Split(T("people.history.header"), "|"), historyRecords(rows))
		}
		fmt.Fprintf(w, "%s  %s\n", st.Ref, st.Name)
		if cl := p.ClassNames(st.ID); len(cl) > 0 {
			fmt.Fprintf(w, "%s %s\n", T("people.student.classes"), strings.Join(cl, ", "))
		}
		if st.Note != "" {
			fmt.Fprintf(w, "%s %s\n", T("people.student.note"), st.Note)
		}
		fmt.Fprintln(w)
		if len(rows) == 0 {
			fmt.Fprintln(w, T("people.history.none"))
			return nil
		}
		return f.write(w, nil, strings.Split(T("people.history.header"), "|"), historyRecords(rows))
	})
}

func newStudentAddCmd() *cobra.Command {
	var class, note string
	cmd := &cobra.Command{
		Use:     "add <id> <name>...",
		Short:   T("people.student.add_short"),
		Example: T("people.student.add_example"),
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				ctx := cmd.Context()
				var c *session.Class
				if class != "" {
					var err error
					if c, err = resolveClass(cmd, s.svc, class); err != nil {
						return err
					}
				}
				st, err := s.svc.AddStudent(ctx, args[0], strings.Join(args[1:], " "), note)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("people.student.added", "id", st.Ref, "name", st.Name))
				if c != nil {
					if _, err := s.svc.Enroll(ctx, c.ID, []session.Enrollee{{Ref: st.Ref, Name: st.Name}}); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), T("people.class.added_one", "id", st.Ref, "class", c.Name))
					return syncClass(ctx, s, c.ID)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&class, "class", "", T("people.flag.class"))
	cmd.Flags().StringVar(&note, "note", "", T("people.flag.note"))
	_ = cmd.RegisterFlagCompletionFunc("class", completeClasses)
	return cmd
}

func newStudentEditCmd() *cobra.Command {
	var name, note string
	cmd := &cobra.Command{
		Use:               "edit <id>",
		Short:             T("people.student.edit_short"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeStudents,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("note") {
				return errors.New(T("people.student.edit_nothing"))
			}
			return peopleCmd(cmd, func(s *store) error {
				st, p, err := resolveStudent(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				if !cmd.Flags().Changed("name") {
					name = st.Name
				}
				if !cmd.Flags().Changed("note") {
					note = st.Note
				}
				st, err = s.svc.EditStudent(cmd.Context(), st.ID, name, note)
				if err != nil {
					return err
				}
				syncClassesOf(cmd.Context(), s, p.Of[st.ID])
				fmt.Fprintln(cmd.OutOrStdout(), T("people.student.edited", "id", st.Ref, "name", st.Name))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", T("people.flag.name"))
	cmd.Flags().StringVar(&note, "note", "", T("people.flag.note"))
	return cmd
}

func newStudentRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "rename <id> <new-id>",
		Short:             T("people.student.rename_short"),
		Long:              T("people.student.rename_long"),
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeStudents,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				st, p, err := resolveStudent(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				old := st.Ref
				h, _ := s.svc.History(cmd.Context(), old)
				st, err = s.svc.RenameStudent(cmd.Context(), st.ID, args[1])
				if err != nil {
					return err
				}
				syncClassesOf(cmd.Context(), s, p.Of[st.ID])
				var sessions []string
				for _, x := range h {
					if x.Session.State == session.StateEnded {
						sessions = append(sessions, x.Session.ID)
					}
				}
				rewriteArchives(cmd.Context(), s, sessions)
				fmt.Fprintln(cmd.OutOrStdout(), T("people.student.renamed", "old", old, "new", st.Ref, "n", len(h)))
				return nil
			})
		},
	}
}

func newStudentMergeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:               "merge <id> <into-id>",
		Short:             T("people.student.merge_short"),
		Long:              T("people.student.merge_long"),
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeStudents,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				ctx := cmd.Context()
				from, p, err := resolveStudent(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				into, err := findStudent(p, args[1])
				if err != nil {
					return err
				}
				fp, err := s.svc.StudentFootprint(ctx, from.ID)
				if err != nil {
					return err
				}
				w := cmd.OutOrStdout()
				fmt.Fprintln(w, T("people.student.merge_plan", "from", from.Ref+" "+from.Name, "into", into.Ref+" "+into.Name,
					"n", len(fp.Sessions), "classes", strings.Join(fp.Classes, ", ")))
				if err := confirmPassword(s.vault); err != nil {
					return err
				}
				if !yes && !confirm(cmd, T("people.student.merge_confirm")) {
					return errors.New(T("cli.cancelled"))
				}
				classes := append(append([]string{}, p.Of[from.ID]...), p.Of[into.ID]...)
				if err := s.svc.MergeStudents(ctx, from.ID, into.ID); err != nil {
					return err
				}
				syncClassesOf(ctx, s, classes)
				rewriteArchives(ctx, s, fp.Sessions)
				fmt.Fprintln(w, T("people.student.merged", "from", from.Ref, "into", into.Ref))
				return nil
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	return cmd
}

func newStudentForgetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:               "forget <id>",
		Short:             T("people.student.forget_short"),
		Long:              T("people.student.forget_long"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeStudents,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				ctx := cmd.Context()
				st, p, err := resolveStudent(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				fp, err := s.svc.StudentFootprint(ctx, st.ID)
				if err != nil {
					return err
				}
				w := cmd.OutOrStdout()
				fmt.Fprintln(w, T("people.student.forget_plan", "who", st.Ref+" "+st.Name, "n", len(fp.Sessions),
					"classes", strings.Join(fp.Classes, ", ")))
				if err := confirmPassword(s.vault); err != nil {
					return err
				}
				if !yes && !confirm(cmd, T("people.student.forget_confirm")) {
					return errors.New(T("cli.cancelled"))
				}
				touched, err := s.svc.ForgetStudent(ctx, st.ID)
				if err != nil {
					return err
				}
				syncClassesOf(ctx, s, p.Of[st.ID])
				rewriteArchives(ctx, s, touched)
				_ = s.db.Compact(ctx)
				fmt.Fprintln(w, T("people.student.forgotten", "who", st.Ref, "n", len(touched)))
				fmt.Fprintln(w, T("people.student.forget_backups"))
				return nil
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	return cmd
}

// ================= ax student history =================

func newHistoryCmd() *cobra.Command {
	var f listFormat
	cmd := &cobra.Command{
		Use:               "history <student>",
		Short:             T("people.history.short"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeStudents,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				st, _, err := resolveStudent(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				h, err := s.svc.History(cmd.Context(), st.Ref)
				if err != nil {
					return err
				}
				rows := historyRows(h)
				if len(rows) == 0 && !f.json && !f.csv {
					fmt.Fprintln(cmd.OutOrStdout(), T("people.history.none"))
					return nil
				}
				return f.write(cmd.OutOrStdout(), rows, strings.Split(T("people.history.header"), "|"), historyRecords(rows))
			})
		},
	}
	f.register(cmd)
	return cmd
}

// ================= ax class =================

func newClassCmd() *cobra.Command {
	var f listFormat
	cmd := &cobra.Command{
		Use:               "class [name]",
		Short:             T("people.class.short"),
		Long:              T("people.class.long"),
		Example:           T("people.class.example"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return listStudents(cmd, args[0], &f)
			}
			return listClasses(cmd, &f)
		},
	}
	f.register(cmd)
	cmd.AddCommand(newClassListCmd(), newClassCreateCmd(), newClassImportCmd(), newClassExportCmd(),
		newClassAddCmd(), newClassRemoveCmd(), newClassRenameCmd(), newClassDeleteCmd())
	return cmd
}

func newClassListCmd() *cobra.Command {
	var f listFormat
	cmd := &cobra.Command{
		Use:   "list",
		Short: T("people.class.list_short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return listClasses(cmd, &f) },
	}
	f.register(cmd)
	return cmd
}

func listClasses(cmd *cobra.Command, f *listFormat) error {
	return peopleCmd(cmd, func(s *store) error {
		p, err := s.svc.People(cmd.Context())
		if err != nil {
			return err
		}
		sessions, err := s.svc.Sessions(cmd.Context())
		if err != nil {
			return err
		}
		type row struct {
			Name     string `json:"name"`
			Students int    `json:"students"`
			Sessions int    `json:"sessions"`
		}
		rows := []row{}
		var recs [][]string
		for _, c := range p.Classes {
			n := 0
			for _, x := range sessions {
				if x.ClassID == c.ID {
					n++
				}
			}
			rows = append(rows, row{Name: c.Name, Students: len(p.Members[c.ID]), Sessions: n})
			recs = append(recs, []string{c.Name, strconv.Itoa(len(p.Members[c.ID])), strconv.Itoa(n)})
		}
		if len(rows) == 0 && !f.json && !f.csv {
			fmt.Fprintln(cmd.OutOrStdout(), T("people.class.none"))
			return nil
		}
		return f.write(cmd.OutOrStdout(), rows, strings.Split(T("people.class.header"), "|"), recs)
	})
}

func newClassCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: T("people.class.create_short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				c, err := s.svc.CreateClass(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("people.class.created", "class", c.Name))
				return syncClass(cmd.Context(), s, c.ID)
			})
		},
	}
}

func newClassRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "rename <name> <new-name>",
		Short:             T("people.class.rename_short"),
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				c, err := resolveClass(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				if err := s.svc.RenameClass(cmd.Context(), c.ID, args[1]); err != nil {
					return err
				}
				_ = s.vault.Remove(classFile(c.Name))
				fmt.Fprintln(cmd.OutOrStdout(), T("people.class.renamed", "old", c.Name, "new", session.NormalizeName(args[1])))
				return syncClass(cmd.Context(), s, c.ID)
			})
		},
	}
}

func newClassDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:               "delete <name>",
		Short:             T("people.class.delete_short"),
		Long:              T("people.class.delete_long"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				c, err := resolveClass(cmd, s.svc, args[0])
				if err != nil {
					return err
				}
				if !yes && !confirm(cmd, T("people.class.delete_confirm", "class", c.Name)) {
					return errors.New(T("cli.cancelled"))
				}
				if err := s.svc.DeleteClass(cmd.Context(), c.ID); err != nil {
					return err
				}
				_ = s.vault.Remove(classFile(c.Name))
				fmt.Fprintln(cmd.OutOrStdout(), T("people.class.deleted", "class", c.Name))
				return nil
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	return cmd
}

func newClassAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "add <class> <student>...",
		Short:             T("people.class.add_short"),
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				p, err := s.svc.People(cmd.Context())
				if err != nil {
					return err
				}
				c, err := findClass(p, args[0])
				if err != nil {
					return err
				}
				var people []session.Enrollee
				for _, ref := range args[1:] {
					st, err := findStudent(p, ref)
					if err != nil {
						return err
					}
					people = append(people, session.Enrollee{Ref: st.Ref, Name: st.Name})
				}
				res, err := s.svc.Enroll(cmd.Context(), c.ID, people)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("people.class.added", "n", res.Added, "class", c.Name, "already", res.Already))
				return syncClass(cmd.Context(), s, c.ID)
			})
		},
	}
}

func newClassRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove <class> <student>...",
		Short:             T("people.class.remove_short"),
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				p, err := s.svc.People(cmd.Context())
				if err != nil {
					return err
				}
				c, err := findClass(p, args[0])
				if err != nil {
					return err
				}
				var ids []string
				for _, ref := range args[1:] {
					st, err := findStudent(p, ref)
					if err != nil {
						return err
					}
					ids = append(ids, st.ID)
				}
				if err := s.svc.Unenroll(cmd.Context(), c.ID, ids...); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("people.class.removed", "n", len(ids), "class", c.Name))
				return syncClass(cmd.Context(), s, c.ID)
			})
		},
	}
}

func newClassExportCmd() *cobra.Command {
	var out, sep string
	cmd := &cobra.Command{
		Use:               "export <class>",
		Short:             T("people.class.export_short"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeClasses,
		RunE: func(cmd *cobra.Command, args []string) error {
			return peopleCmd(cmd, func(s *store) error {
				p, err := s.svc.People(cmd.Context())
				if err != nil {
					return err
				}
				c, err := findClass(p, args[0])
				if err != nil {
					return err
				}
				w := cmd.OutOrStdout()
				if out != "" {
					fh, err := os.Create(out)
					if err != nil {
						return err
					}
					defer fh.Close()
					w = fh
					if cfg.Bool("results.bom") {
						_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
					}
				}
				cw := csv.NewWriter(w)
				cw.Comma = []rune(sep)[0]
				_ = cw.Write([]string{"id", "name"})
				for _, id := range p.Members[c.ID] {
					_ = cw.Write([]string{p.ByID[id].Ref, p.ByID[id].Name})
				}
				cw.Flush()
				if out != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), T("people.class.exported", "n", len(p.Members[c.ID]), "file", out))
				}
				return cw.Error()
			})
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", T("cli.results.flag_out"))
	cmd.Flags().StringVar(&sep, "sep", cfg.Str("results.separator"), T("cli.results.flag_sep"))
	return cmd
}

// ---- import ----

// rosterRow is a row read from a class list.
type rosterRow struct {
	Line int
	Ref  string
	Name string
}

// decodeText returns data as UTF-8: without its BOM, or converted from
// Windows-1252 (what Excel writes in many countries) when not UTF-8.
func decodeText(data []byte) (string, string) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data), "UTF-8"
	}
	out, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil {
		return string(data), "?"
	}
	return string(out), "Windows-1252"
}

// detectSep picks the separator that splits the first lines most evenly.
func detectSep(text string) rune {
	lines := strings.SplitN(text, "\n", 6)
	best, bestScore := ',', -1
	for _, sep := range []rune{';', ',', '\t', '|'} {
		counts := map[int]int{}
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				counts[strings.Count(l, string(sep))]++
			}
		}
		for n, c := range counts {
			if n > 0 && c*10+n > bestScore {
				best, bestScore = sep, c*10+n
			}
		}
	}
	return best
}

var (
	idHeaders    = []string{"id", "studentid", "identificador", "codigo", "matricula", "dni", "nie", "numero", "expediente", "nia", "carnet", "legajo"}
	nameHeaders  = []string{"name", "fullname", "nombre", "nombrecompleto", "nombreyapellidos", "alumno", "alumna", "estudiante", "student", "apellidosynombre"}
	firstHeaders = []string{"firstname", "nombre", "nombres", "givenname"}
	lastHeaders  = []string{"lastname", "apellidos", "apellido", "surname", "familyname"}
)

func headerIndex(header []string, names []string) int {
	for i, h := range header {
		for _, n := range names {
			if search.Fold(h) == n {
				return i
			}
		}
	}
	return -1
}

// parseRoster reads a class list: a header naming the columns (id and
// name, or first and last names), or two columns id, name.
func parseRoster(data []byte) ([]rosterRow, string, rune, error) {
	text, enc := decodeText(data)
	sep := detectSep(text)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma, r.FieldsPerRecord, r.TrimLeadingSpace, r.LazyQuotes = sep, -1, true, true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, enc, sep, err
	}
	if len(recs) == 0 {
		return nil, enc, sep, errors.New(T("people.import.empty"))
	}
	idCol, nameCol, firstCol, lastCol := 0, 1, -1, -1
	start := 0
	h := recs[0]
	if i := headerIndex(h, idHeaders); i >= 0 {
		idCol, start = i, 1
		first, last := headerIndex(h, firstHeaders), headerIndex(h, lastHeaders)
		switch n := headerIndex(h, nameHeaders); {
		case first >= 0 && last >= 0:
			firstCol, lastCol, nameCol = first, last, -1
		case n >= 0:
			nameCol = n
		default:
			nameCol = (idCol + 1) % max(2, len(h))
		}
	}
	get := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var rows []rosterRow
	for i, rec := range recs[start:] {
		row := rosterRow{Line: i + start + 1, Ref: get(rec, idCol), Name: get(rec, nameCol)}
		if firstCol >= 0 {
			row.Name = strings.TrimSpace(get(rec, firstCol) + " " + get(rec, lastCol))
		}
		if row.Ref == "" && row.Name == "" {
			continue
		}
		rows = append(rows, row)
	}
	return rows, enc, sep, nil
}

// rosterPreview sorts the rows of a class list against the vault.
type rosterPreview struct {
	Rows      []rosterRow
	New       []rosterRow
	Known     []rosterRow
	Different []string // known identifier, different name
	Repeated  []string // identifier twice in the file
	Invalid   []rosterRow
}

func previewRoster(rows []rosterRow, p *session.People) *rosterPreview {
	pv := &rosterPreview{Rows: rows}
	seen := map[string]bool{}
	for _, r := range rows {
		ref := session.NormalizeStudentID(r.Ref)
		if ref == "" || session.NormalizeName(r.Name) == "" {
			pv.Invalid = append(pv.Invalid, r)
			continue
		}
		if seen[ref] {
			pv.Repeated = append(pv.Repeated, ref)
			continue
		}
		seen[ref] = true
		if st := p.ByRef[ref]; st != nil {
			pv.Known = append(pv.Known, r)
			if st.Name != session.NormalizeName(r.Name) {
				pv.Different = append(pv.Different, fmt.Sprintf("%s: %q → %q", ref, st.Name, r.Name))
			}
		} else {
			pv.New = append(pv.New, r)
		}
	}
	return pv
}

func printPreview(w io.Writer, pv *rosterPreview, enc string, sep rune) {
	sepName := map[rune]string{',': ",", ';': ";", '\t': "TAB", '|': "|"}[sep]
	fmt.Fprintln(w, T("people.import.read", "n", len(pv.Rows), "enc", enc, "sep", sepName))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i, r := range pv.Rows {
		if i == 8 {
			fmt.Fprintln(tw, "  …")
			break
		}
		fmt.Fprintf(tw, "  %s\t%s\n", session.NormalizeStudentID(r.Ref), r.Name)
	}
	_ = tw.Flush()
	fmt.Fprintln(w, T("people.import.counts", "new", len(pv.New), "known", len(pv.Known)))
	for _, d := range pv.Different {
		fmt.Fprintln(w, "  "+T("people.import.different", "what", d))
	}
	for _, r := range pv.Repeated {
		fmt.Fprintln(w, "  "+T("people.import.repeated", "id", r))
	}
	for _, r := range pv.Invalid {
		fmt.Fprintln(w, "  "+T("people.import.invalid", "line", r.Line))
	}
}

func newClassImportCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "import <class> <file.csv>",
		Short:   T("people.class.import_short"),
		Long:    T("people.class.import_long"),
		Example: T("people.class.import_example"),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			rows, enc, sep, err := parseRoster(data)
			if err != nil {
				return err
			}
			return peopleCmd(cmd, func(s *store) error {
				ctx := cmd.Context()
				p, err := s.svc.People(ctx)
				if err != nil {
					return err
				}
				pv := previewRoster(rows, p)
				w := cmd.OutOrStdout()
				printPreview(w, pv, enc, sep)
				name, err := session.CheckClassName(args[0])
				if err != nil {
					return err
				}
				c := p.Class(name)
				if c == nil {
					fmt.Fprintln(w, T("people.import.new_class", "class", name))
				}
				if !yes && !confirm(cmd, T("people.import.confirm", "n", len(pv.New)+len(pv.Known), "class", name)) {
					return errors.New(T("cli.cancelled"))
				}
				if c == nil {
					if c, err = s.svc.CreateClass(ctx, name); err != nil {
						return err
					}
				}
				var people []session.Enrollee
				for _, r := range append(append([]rosterRow{}, pv.New...), pv.Known...) {
					people = append(people, session.Enrollee{Ref: r.Ref, Name: r.Name})
				}
				res, err := s.svc.Enroll(ctx, c.ID, people)
				if err != nil {
					return err
				}
				fmt.Fprintln(w, T("people.import.done", "created", res.Created, "added", res.Added, "already", res.Already, "class", c.Name))
				return syncClass(ctx, s, c.ID)
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	return cmd
}
