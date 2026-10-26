package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/backup"
	"github.com/lisk0vian/assess/internal/config"
)

func newBackupCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:     "backup",
		Short:   T("backup.short"),
		Long:    T("backup.long"),
		Example: T("backup.example"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			// The files are copied as they are: no key is needed.
			s, err := openStore(cmd.Context(), dir, false)
			if err != nil {
				return err
			}
			defer s.Close()
			file, err := backup.Create(cmd.Context(), s.vault, s.db, out, backup.ManualPrefix)
			if err != nil {
				return err
			}
			info, _ := os.Stat(file)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("backup.done", "file", file, "size", humanSize(size)))
			if s.vault.Encrypted() {
				fmt.Fprintln(cmd.OutOrStdout(), T("backup.encrypted_note"))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", T("backup.flag_out"))
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: T("backup.list_short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			v, _, err := activeVault(dir)
			if err != nil {
				return err
			}
			list, err := backup.List(v)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), T("backup.none"))
				return nil
			}
			for _, f := range list {
				info, err := os.Stat(f)
				if err != nil {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %8s  %s\n", info.ModTime().Format("2006-01-02 15:04"), humanSize(info.Size()), f)
			}
			return nil
		},
	})
	return cmd
}

func newRestoreCmd() *cobra.Command {
	var name string
	var use bool
	cmd := &cobra.Command{
		Use:     "restore <backup.zip> <new-folder>",
		Short:   T("restore.short"),
		Long:    T("restore.long"),
		Example: T("restore.example"),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			v, err := backup.Restore(args[0], args[1])
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(v.Dir)
			}
			reg := loadRegistry(dir)
			name = reg.Add(name, v.Dir)
			if err := reg.Save(); err != nil {
				return err
			}
			if v.Meta.Name != name {
				_ = v.SetName(name)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, T("restore.done", "dir", v.Dir, "name", name))
			if v.Encrypted() {
				fmt.Fprintln(w, T("restore.encrypted_note"))
			}
			if use {
				if err := config.Set(cfg.Path, "vault.path", name); err != nil {
					return err
				}
				fmt.Fprintln(w, T("vault.use.done", "name", name))
			} else {
				fmt.Fprintln(w, T("vault.init.use_hint", "name", name))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", T("vault.init.flag_name"))
	cmd.Flags().BoolVar(&use, "use", false, T("vault.init.flag_use"))
	return cmd
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
