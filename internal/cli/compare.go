package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/archive"
	"github.com/lisk0vian/assess/internal/report"
	"github.com/lisk0vian/assess/internal/session"
)

// compareTop is how many questions the table shows unless --all.
const compareTop = 10

// compareSame is the gap, as a share, under which no question sets the classes apart.
const compareSame = 0.1

func newExamCompareCmd() *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:     "compare <exam> [class...]",
		Short:   T("cli.compare.short"),
		Long:    T("cli.compare.long"),
		Example: "ax exam compare algebra-01\nax exam compare algebra-01 3B 3C",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			var c *report.Comparison
			err = withService(cmd.Context(), dir, func(svc *session.Service) error {
				var err error
				c, err = compareGroups(cmd, svc, args[0], args[1:])
				return err
			})
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(c)
			}
			printComparison(cmd.OutOrStdout(), c, all)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, T("cli.flag.json"))
	cmd.Flags().BoolVar(&all, "all", false, T("cli.compare.flag_all"))
	return cmd
}

// compareGroups takes, for each class, its latest finished session of the
// exam, and compares them. classes narrows the groups; empty is every class.
func compareGroups(cmd *cobra.Command, svc *session.Service, examRef string, classes []string) (*report.Comparison, error) {
	ctx := cmd.Context()
	all, err := svc.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	matches := func(x *session.Overview) bool {
		return strings.EqualFold(x.ExamID, examRef) || strings.EqualFold(x.ExamTitle, examRef)
	}
	wanted := func(name string) bool {
		return len(classes) == 0 || slicesContainsFold(classes, name)
	}
	latest := map[string]*session.Overview{}
	var names []string
	known := false
	for _, x := range all { // oldest first: the last one of a class wins
		if !matches(x) {
			continue
		}
		known = true
		if x.State != session.StateEnded || x.ClassName == "" || !wanted(x.ClassName) {
			continue
		}
		if latest[x.ClassName] == nil {
			names = append(names, x.ClassName)
		}
		latest[x.ClassName] = x
	}
	if !known {
		return nil, errors.New(T("cli.compare.no_exam", "exam", examRef))
	}
	for _, c := range classes {
		if !slicesContainsFold(names, c) {
			return nil, errors.New(T("cli.compare.no_class", "class", c, "exam", examRef))
		}
	}
	if len(names) < 2 {
		return nil, errors.New(T("cli.compare.need_two", "exam", examRef))
	}
	var reps []*report.Report
	for _, n := range names {
		r, _, err := archive.BuildReport(ctx, svc, latest[n].ID)
		if err != nil {
			return nil, err
		}
		reps = append(reps, r)
	}
	return report.Compare(names, reps), nil
}

func slicesContainsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(s)) {
			return true
		}
	}
	return false
}

func pctText(f float64) string { return strconv.FormatFloat(round1(f), 'f', -1, 64) + " %" }

func printComparison(w io.Writer, c *report.Comparison, all bool) {
	fmt.Fprintln(w, T("cli.compare.title", "title", c.ExamTitle, "n", len(c.Groups)))
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.ReplaceAll(T("cli.compare.groups_header"), ",", "\t"))
	for _, g := range c.Groups {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", g.Name, g.Date.Local().Format("2006-01-02"), g.Submitted, pctText(g.Mean), pctText(g.Median))
	}
	_ = tw.Flush()

	fmt.Fprintln(w)
	if len(c.Questions) == 0 || c.Questions[0].Gap < compareSame {
		fmt.Fprintln(w, T("cli.compare.same"))
	} else {
		fmt.Fprintln(w, T("cli.compare.differ"))
	}
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	head := []string{T("cli.compare.question")}
	for _, g := range c.Groups {
		head = append(head, g.Name)
	}
	fmt.Fprintln(tw, strings.Join(append(head, T("cli.compare.gap"), ""), "\t"))
	for i, q := range c.Questions {
		if !all && i == compareTop {
			fmt.Fprintln(tw, T("cli.compare.more", "n", len(c.Questions)-compareTop))
			break
		}
		row := []string{q.ID}
		for _, r := range q.Rates {
			if r < 0 {
				row = append(row, "—")
			} else {
				row = append(row, pctText(r*100))
			}
		}
		prompt := []rune(q.Prompt)
		if len(prompt) > 50 {
			prompt = append(prompt[:49], '…')
		}
		fmt.Fprintln(tw, strings.Join(append(row, strconv.FormatFloat(round1(q.Gap*100), 'f', -1, 64), string(prompt)), "\t"))
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
	fmt.Fprintln(w, T("cli.compare.caution"))
}
