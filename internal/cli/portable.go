package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/backup"
	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/vault"
)

func newPortableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "portable",
		Short: T("portable.short"),
		Long:  T("portable.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if cfg.Portable() {
				dir, _ := dataDir()
				fmt.Fprintln(w, T("portable.is_portable", "dir", cfg.Dir(), "data", dir))
				return nil
			}
			fmt.Fprintln(w, T("portable.not_portable", "path", cfg.Path))
			return nil
		},
	}
	cmd.AddCommand(newPortableInitCmd())
	return cmd
}

func newPortableInitCmd() *cobra.Command {
	var copyVault bool
	cmd := &cobra.Command{
		Use:     "init <folder>",
		Short:   T("portable.init_short"),
		Long:    T("portable.init_long"),
		Example: T("portable.init_example"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			settings := filepath.Join(dest, config.PortableFile)
			if _, err := os.Stat(settings); err == nil {
				return errors.New(T("portable.exists", "path", settings))
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			name := filepath.Base(exe)
			target := filepath.Join(dest, name)
			if !vault.SamePath(filepath.Dir(exe), dest) {
				if err := copyFile(exe, target); err != nil {
					return err
				}
			}
			if err := config.Create(settings, cfg.Language()); err != nil {
				return err
			}
			if err := config.Set(settings, "data_dir", "data"); err != nil {
				return err
			}
			if cfg.Str("language") != "en" {
				_ = config.Set(settings, "language", cfg.Str("language"))
			}
			w := cmd.OutOrStdout()
			if copyVault {
				dir, err := dataDir()
				if err != nil {
					return err
				}
				s, err := openStore(cmd.Context(), dir, false)
				if err != nil {
					return err
				}
				zip, err := backup.Create(cmd.Context(), s.vault, s.db, filepath.Join(os.TempDir(), "ax-portable-backup.zip"), backup.ManualPrefix)
				s.Close()
				if err != nil {
					return err
				}
				defer func() { _ = os.Remove(zip) }()
				if _, err := backup.Restore(zip, vault.DefaultDir(filepath.Join(dest, "data"))); err != nil {
					return err
				}
				fmt.Fprintln(w, T("portable.vault_copied", "name", s.vault.Meta.Name))
			}
			fmt.Fprintln(w, T("portable.done", "dir", dest, "exe", target))
			return nil
		},
	}
	cmd.Flags().BoolVar(&copyVault, "copy-vault", false, T("portable.flag_copy_vault"))
	return cmd
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755) //nolint:gosec // G302: a program must be executable
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
