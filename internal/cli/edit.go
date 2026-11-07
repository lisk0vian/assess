package cli

import (
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/tui"
)

func newEditCmd() *cobra.Command {
	var sf serveFlags
	var duration time.Duration
	cmd := &cobra.Command{
		Use:               "edit <exam>",
		Short:             T("cli.edit.short"),
		Long:              T("cli.edit.long"),
		Example:           T("cli.edit.example"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeExams,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminal() {
				return errors.New(T("cli.edit.no_terminal"))
			}
			return editExam(cmd, &sf, args[0], duration)
		},
	}
	sf.register(cmd)
	cmd.Flags().DurationVar(&duration, "duration", 0, T("cli.edit.flag_duration"))
	return cmd
}

// editExam runs the editor on an exam folder, or on an exam of the vault
// (unpacked into a temporary folder and packed again), and starts the exam
// if asked to. A folder whose exam is also in the vault refreshes the copy.
func editExam(cmd *cobra.Command, sf *serveFlags, ref string, duration time.Duration) error {
	dataDir, err := dataDir()
	if err != nil {
		return err
	}
	dir, cleanup, err := examFolder(cmd, dataDir, ref)
	if err != nil {
		return err
	}
	defer cleanup()
	d, err := exam.OpenDraft(dir)
	if err != nil {
		return err
	}
	m := tui.NewEditor(d, cfg.Str("editor"))
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	for m.Try { // rehearse in the browser, then back to the editor
		if err := tryExam(cmd, dir, nil); err != nil {
			return err
		}
		if err := d.Reload(); err != nil {
			return err
		}
		m = tui.NewEditor(d, cfg.Str("editor"))
		if _, err := tea.NewProgram(m).Run(); err != nil {
			return err
		}
	}
	out := cmd.OutOrStdout()
	inVault := dir != ref
	if v, _, err := activeVault(dataDir); err == nil {
		if ex, _, err := exam.Load(dir); err == nil && (inVault || v.Exists(examFile(ex.Manifest.ID))) {
			if _, err := storeExam(v, dir, true); err != nil {
				return err
			}
			fmt.Fprintln(out, T("vault.edit.saved", "name", v.Meta.Name))
		}
	}
	if !inVault {
		fmt.Fprintln(out, T("cli.edit.saved", "dir", shellQuote(dir)))
	}
	if !m.Start {
		return nil
	}
	if duration != 0 {
		return startExam(cmd, sf, dir, duration, false)
	}
	return prepareAndStart(cmd, dataDir, dir, sf)
}

// editVaultExam edits an exam of the vault from Explore.
func editVaultExam(cmd *cobra.Command, _ string, id string) error {
	return editExam(cmd, defaultServeFlags(), id, 0)
}
