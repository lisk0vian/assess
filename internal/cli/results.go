package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/archive"
	"github.com/lisk0vian/assess/internal/session"
)

func newResultsCmd() *cobra.Command {
	var format, out, sessionID, sep string
	var noBOM bool
	cmd := &cobra.Command{
		Use:     "results",
		Short:   T("cli.results.short"),
		Long:    T("cli.results.long"),
		Example: T("cli.results.example"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "csv" && format != "json" {
				return errors.New(T("cli.results.bad_format", "format", format))
			}
			if len([]rune(sep)) != 1 {
				return errors.New(T("cli.results.bad_sep"))
			}
			dir, err := dataDir()
			if err != nil {
				return err
			}
			var res *session.Results
			err = withService(cmd.Context(), dir, func(svc *session.Service) error {
				info, err := resolveSession(cmd, svc, sessionID)
				if err != nil {
					return err
				}
				res, err = svc.Results(cmd.Context(), info.ID)
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
			if cfg.Bool("results.bom") && !noBOM {
				_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM: lets Excel read accents
			}
			if err := archive.CSV(w, res, []rune(sep)[0]); err != nil {
				return err
			}
			if out != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), T("cli.results.wrote", "n", len(res.Rows), "file", out))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", cfg.Str("results.format"), T("cli.results.flag_format"))
	cmd.Flags().StringVarP(&out, "out", "o", "", T("cli.results.flag_out"))
	cmd.Flags().StringVar(&sessionID, "session", "", T("cli.flag.session"))
	cmd.Flags().StringVar(&sep, "sep", cfg.Str("results.separator"), T("cli.results.flag_sep"))
	cmd.Flags().BoolVar(&noBOM, "no-bom", false, T("cli.results.flag_no_bom"))
	return cmd
}
