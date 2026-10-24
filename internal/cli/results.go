package cli

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/session"
)

func newResultsCmd() *cobra.Command {
	var format, out, sessionID, sep string
	var noBOM bool
	cmd := &cobra.Command{
		Use:   "results",
		Short: "Export the results of a session as CSV or JSON",
		Long: "Results exports one row per student: score, submission time, reason and\n" +
			"the points of every question they received. It reads the database directly,\n" +
			"so it works while the server runs or after it stopped. Defaults to the most\n" +
			"recent session.",
		Example: "  ax results > notas.csv\n  ax results --sep \";\" --out notas.csv   # Excel with a Spanish locale\n  ax results --format json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "csv" && format != "json" {
				return fmt.Errorf("unknown format %q (use csv or json)", format)
			}
			if len([]rune(sep)) != 1 {
				return errors.New("--sep must be a single character")
			}
			dir, err := dataDir()
			if err != nil {
				return err
			}
			var res *session.Results
			err = withService(cmd.Context(), dir, func(svc *session.Service) error {
				id := sessionID
				if id == "" {
					latest, err := svc.LatestSession(cmd.Context())
					if errors.Is(err, session.ErrNotFound) {
						return errors.New("there are no sessions yet")
					}
					if err != nil {
						return err
					}
					id = latest.ID
				}
				res, err = svc.Results(cmd.Context(), id)
				return err
			})
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if out != "" {
				f, err := os.Create(out)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}
			if format == "json" {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}
			if !noBOM {
				_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM: lets Excel read accents
			}
			if err := writeCSV(w, res, []rune(sep)[0]); err != nil {
				return err
			}
			if out != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %d rows to %s\n", len(res.Rows), out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "csv", "csv or json")
	cmd.Flags().StringVarP(&out, "out", "o", "", "write to a file instead of stdout")
	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: the most recent)")
	cmd.Flags().StringVar(&sep, "sep", ",", "CSV field separator")
	cmd.Flags().BoolVar(&noBOM, "no-bom", false, "omit the UTF-8 byte order mark")
	return cmd
}

func writeCSV(w io.Writer, res *session.Results, sep rune) error {
	cw := csv.NewWriter(w)
	cw.Comma = sep
	header := []string{"student_id", "name", "state", "score", "max_score", "percent", "submitted_at", "submit_reason", "minutes"}
	header = append(header, res.Questions...)
	if err := cw.Write(header); err != nil {
		return err
	}
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	for _, r := range res.Rows {
		rec := []string{r.StudentID, r.Name, string(r.State), "", num(r.MaxScore), "", "", string(r.SubmitReason), ""}
		if r.Score != nil {
			rec[3] = num(*r.Score)
			if r.MaxScore > 0 {
				rec[5] = num(float64(int(*r.Score/r.MaxScore*1000+0.5)) / 10)
			}
		}
		if !r.SubmittedAt.IsZero() {
			rec[6] = r.SubmittedAt.Local().Format(time.DateTime)
			rec[8] = num(float64(int(r.Minutes*10+0.5)) / 10)
		}
		for _, q := range res.Questions {
			if p, ok := r.Points[q]; ok {
				rec = append(rec, num(p))
			} else {
				rec = append(rec, "") // the question was not in this student's exam (pools)
			}
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
