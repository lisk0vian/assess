package cli

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/search"
	"github.com/lisk0vian/assess/internal/session"
)

// findHit is one result of `ax find`.
type findHit struct {
	Type   string `json:"type"` // student, class, exam, session
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	score  int
}

type findFilters struct {
	types   []string
	class   string
	exam    string
	since   string
	below   float64
	flagged bool
}

func (f *findFilters) wants(t string) bool {
	if len(f.types) == 0 {
		return true
	}
	for _, x := range f.types {
		if strings.HasPrefix(t, strings.TrimSuffix(strings.ToLower(x), "s")) {
			return true
		}
	}
	return false
}

// needsHistory reports whether a student filter looks at their sessions.
func (f *findFilters) needsHistory() bool {
	return f.exam != "" || f.since != "" || f.below > 0 || f.flagged
}

func newFindCmd() *cobra.Command {
	var lf listFormat
	var ff findFilters
	cmd := &cobra.Command{
		Use:     "find [text]",
		Short:   T("find.short"),
		Long:    T("find.long"),
		Example: T("find.example"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			var since time.Time
			if ff.since != "" {
				t, err := time.ParseInLocation("2006-01-02", ff.since, time.Local)
				if err != nil {
					return errors.New(T("find.bad_date", "date", ff.since))
				}
				since = t
			}
			return peopleCmd(cmd, func(s *store) error {
				hits, err := find(cmd, s, query, &ff, since)
				if err != nil {
					return err
				}
				if len(hits) == 0 && !lf.json && !lf.csv {
					fmt.Fprintln(cmd.OutOrStdout(), T("find.none", "q", query))
					return nil
				}
				var recs [][]string
				for _, h := range hits {
					recs = append(recs, []string{T("find.type." + h.Type), h.Key, h.Label, h.Detail})
				}
				return lf.write(cmd.OutOrStdout(), hits, strings.Split(T("find.header"), "|"), recs)
			})
		},
	}
	lf.register(cmd)
	cmd.Flags().StringSliceVar(&ff.types, "type", nil, T("find.flag_type"))
	cmd.Flags().StringVar(&ff.class, "class", "", T("find.flag_class"))
	cmd.Flags().StringVar(&ff.exam, "exam", "", T("find.flag_exam"))
	cmd.Flags().StringVar(&ff.since, "since", "", T("find.flag_since"))
	cmd.Flags().Float64Var(&ff.below, "below", 0, T("find.flag_below"))
	cmd.Flags().BoolVar(&ff.flagged, "flagged", false, T("find.flag_flagged"))
	_ = cmd.RegisterFlagCompletionFunc("class", completeClasses)
	_ = cmd.RegisterFlagCompletionFunc("type", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"student", "class", "exam", "session"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func find(cmd *cobra.Command, s *store, query string, ff *findFilters, since time.Time) ([]findHit, error) {
	ctx := cmd.Context()
	p, err := s.svc.People(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := s.svc.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	var class *session.Class
	if ff.class != "" {
		if class, err = findClass(p, ff.class); err != nil {
			return nil, err
		}
	}
	examMatch := func(id, title string) bool {
		return ff.exam == "" || strings.EqualFold(id, ff.exam) || search.Score(ff.exam, id, title) >= 60
	}
	var out []findHit
	add := func(h findHit, score int) {
		if score > 0 {
			h.score = score
			out = append(out, h)
		}
	}

	if ff.wants("student") {
		for _, st := range p.List {
			classes := p.ClassNames(st.ID)
			score := search.Score(query, st.Ref, st.Name, strings.Join(classes, " "))
			if score == 0 || (class != nil && !slices.Contains(p.Of[st.ID], class.ID)) {
				continue
			}
			detail := strings.Join(classes, ", ")
			if ff.needsHistory() {
				h, err := s.svc.History(ctx, st.Ref)
				if err != nil {
					return nil, err
				}
				ok, note := matchHistory(h, ff, since, examMatch)
				if !ok {
					continue
				}
				if note != "" {
					detail = strings.TrimPrefix(detail+" · "+note, " · ")
				}
			}
			add(findHit{Type: "student", Key: st.Ref, Label: st.Name, Detail: detail}, score)
		}
	}
	if ff.wants("class") && ff.class == "" && !ff.needsHistory() {
		for _, c := range p.Classes {
			add(findHit{Type: "class", Key: c.Name, Label: T("people.n_students", "n", len(p.Members[c.ID]))}, search.Score(query, c.Name))
		}
	}
	if ff.wants("exam") && ff.class == "" && !ff.flagged && ff.below == 0 {
		exams, err := vaultExams(s.vault)
		if err != nil {
			return nil, err
		}
		withUsage(exams, sessions)
		for _, e := range exams {
			if !examMatch(e.ID, e.Title) {
				continue
			}
			add(findHit{Type: "exam", Key: e.ID, Label: e.Title, Detail: T("find.exam_detail", "n", e.Sessions)}, search.Score(query, e.ID, e.Title))
		}
	}
	if ff.wants("session") && !ff.flagged {
		for i := len(sessions) - 1; i >= 0; i-- {
			x := sessions[i]
			if (class != nil && x.ClassID != class.ID) || !examMatch(x.ExamID, x.ExamTitle) || (!since.IsZero() && x.CreatedAt.Before(since)) {
				continue
			}
			if ff.below > 0 && (x.Submitted == 0 || x.Average >= ff.below) {
				continue
			}
			it := sessionItem(x)
			detail := it.Detail
			if x.Submitted > 0 {
				detail += " · " + T("find.average", "pct", strconv.FormatFloat(round1(x.Average), 'f', -1, 64))
			}
			add(findHit{Type: "session", Key: x.Code, Label: x.ExamTitle, Detail: detail},
				search.Score(query, x.Code, x.ExamTitle, x.ExamID, x.ClassName, x.CreatedAt.Local().Format("2006-01-02")))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out, nil
}

// matchHistory applies the session filters to a student's history and
// says why they matched.
func matchHistory(h []*session.Participation, ff *findFilters, since time.Time, examMatch func(id, title string) bool) (bool, string) {
	var notes []string
	matched := false
	for _, x := range h {
		if !examMatch(x.ExamID, x.ExamTitle) || (!since.IsZero() && x.Session.CreatedAt.Before(since)) {
			continue
		}
		a := x.Attempt
		if ff.below > 0 {
			if a == nil || a.SubmittedAt.IsZero() || a.MaxScore == 0 || a.Score/a.MaxScore*100 >= ff.below {
				continue
			}
			notes = append(notes, x.ExamTitle+" "+strconv.FormatFloat(round1(a.Score/a.MaxScore*100), 'f', -1, 64)+"%")
		}
		if ff.flagged {
			if x.Strikes == 0 && x.Signals == 0 {
				continue
			}
			notes = append(notes, T("find.flagged_note", "exam", x.ExamTitle, "strikes", x.Strikes, "signals", x.Signals))
		}
		matched = true
	}
	return matched, strings.Join(notes, "; ")
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
