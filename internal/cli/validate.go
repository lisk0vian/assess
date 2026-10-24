package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
)

// errSilent signals a failure already reported to the user.
var errSilent = errors.New("")

func newValidateCmd() *cobra.Command {
	var asJSON, strict bool
	cmd := &cobra.Command{
		Use:   "validate <exam-folder>...",
		Short: "Check exam folders for errors before publishing them",
		Long: "Validate checks every file of an exam folder against the assess/v1 format:\n" +
			"unknown fields, ids, references, answer keys, pools, assets and limits.\n" +
			"It exits with status 1 when any folder has errors.",
		Example: "  ax validate ./exams/algebra-01\n  ax validate --strict exams/*",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var reports []validateReport
			failed := false
			for _, dir := range args {
				r, err := validateDir(dir, strict)
				if err != nil {
					return err
				}
				failed = failed || !r.Valid
				reports = append(reports, r)
			}
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(reports); err != nil {
					return err
				}
			} else {
				for i, r := range reports {
					if i > 0 {
						fmt.Fprintln(out)
					}
					printReport(out, r)
				}
			}
			if failed {
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print results as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	return cmd
}

type validateReport struct {
	Path     string        `json:"path"`
	ID       string        `json:"id,omitempty"`
	Valid    bool          `json:"valid"`
	Errors   int           `json:"errors"`
	Warnings int           `json:"warnings"`
	Problems exam.Problems `json:"problems"`
	Stats    *exam.Stats   `json:"stats,omitempty"`
}

func validateDir(dir string, strict bool) (validateReport, error) {
	ex, probs, err := exam.Load(dir)
	if err != nil {
		return validateReport{}, fmt.Errorf("cannot open exam: %w", err)
	}
	if probs == nil {
		probs = exam.Problems{}
	}
	r := validateReport{
		Path:     dir,
		ID:       ex.Manifest.ID,
		Errors:   probs.Count(exam.SeverityError),
		Warnings: probs.Count(exam.SeverityWarning),
		Problems: probs,
	}
	r.Valid = r.Errors == 0 && (!strict || r.Warnings == 0)
	if r.Errors == 0 {
		s := ex.Stats()
		r.Stats = &s
	}
	return r, nil
}

func printReport(w io.Writer, r validateReport) {
	for _, p := range r.Problems {
		loc := filepath.ToSlash(filepath.Join(r.Path, filepath.FromSlash(p.File)))
		if p.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, p.Line)
		}
		field := ""
		if p.Field != "" {
			field = p.Field + ": "
		}
		fmt.Fprintf(w, "%s: %s: %s%s\n", loc, p.Severity, field, p.Message)
	}
	name := r.ID
	if name == "" {
		name = r.Path
	}
	switch {
	case r.Valid && r.Stats != nil:
		fmt.Fprintf(w, "ok  %s: %d questions, %d per attempt, %s points per attempt",
			name, r.Stats.TotalQuestions, r.Stats.QuestionsPerAttempt, exam.FormatPoints(r.Stats.PointsPerAttempt))
		if r.Warnings > 0 {
			fmt.Fprintf(w, " (%s)", plural(r.Warnings, "warning"))
		}
		fmt.Fprintln(w)
	default:
		fmt.Fprintf(w, "FAIL %s: %s, %s\n", name, plural(r.Errors, "error"), plural(r.Warnings, "warning"))
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
