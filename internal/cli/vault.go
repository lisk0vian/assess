package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/backup"
	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/examx"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/storage/sqlite"
	"github.com/lisk0vian/assess/internal/vault"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: T("vault.cmd.short"),
		Long:  T("vault.cmd.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return vaultInfo(cmd)
		},
	}
	cmd.AddCommand(newVaultInitCmd(), newVaultListCmd(), newVaultUseCmd(), newVaultLockCmd(), newVaultUnlockCmd(),
		newVaultEncryptCmd(), newVaultDecryptCmd(), newVaultPasswdCmd(), newVaultAddCmd(), newVaultExportCmd(),
		newVaultForgetCmd(), newVaultAgentCmd())
	return cmd
}

// askNewPassword asks a new password twice.
func askNewPassword() (string, error) {
	for range 3 {
		pw, err := readSecret(T("vault.new_password"))
		if err != nil {
			return "", err
		}
		if len([]rune(pw)) < vault.MinPassword {
			fmt.Fprintln(os.Stderr, T("vault.short_password", "n", vault.MinPassword))
			continue
		}
		again, err := readSecret(T("vault.repeat_password"))
		if err != nil {
			return "", err
		}
		if pw == again {
			return pw, nil
		}
		fmt.Fprintln(os.Stderr, T("vault.mismatch"))
	}
	return "", errors.New(T("vault.no_password"))
}

// showRecovery prints the recovery key once and waits for the teacher to
// confirm they kept it.
func showRecovery(cmd *cobra.Command, key string) {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w)
	fmt.Fprintln(w, T("vault.recovery.title"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "    "+key)
	fmt.Fprintln(w)
	fmt.Fprintln(w, T("vault.recovery.body"))
	if isTerminal() {
		fmt.Fprint(w, T("vault.recovery.enter"))
		_, _ = readLine(cmd)
	}
}

func readLine(cmd *cobra.Command) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := cmd.InOrStdin().Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return strings.TrimRight(b.String(), "\r"), nil
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			return b.String(), err
		}
	}
}

// noSessionRunning refuses to rewrite the database while a server uses it.
func noSessionRunning(dir string) error {
	if h, _, _ := daemon.Check(dir); h == daemon.Running {
		return errors.New(T("vault.busy"))
	}
	return nil
}

// ---------------- ax vault (info) ----------------

func vaultInfo(cmd *cobra.Command) error {
	dir, err := dataDir()
	if err != nil {
		return err
	}
	v, src, err := activeVault(dir)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s %s\n", T("vault.info.name"), v.Meta.Name)
	fmt.Fprintf(w, "%s %s\n", T("vault.info.folder"), v.Dir)
	fmt.Fprintf(w, "%s %s\n", T("vault.info.chosen"), T("vault.source."+string(src)))
	fmt.Fprintf(w, "%s %s\n", T("vault.info.state"), vaultState(cmd.Context(), dir, v))
	ok := tryUnlock(cmd.Context(), dir, v)
	fmt.Fprintf(w, "%s %d\n", T("vault.info.exams"), len(examIDs(v)))
	_ = withLockedService(cmd.Context(), dir, func(svc *session.Service) error {
		all, err := svc.Sessions(cmd.Context())
		if err == nil {
			fmt.Fprintf(w, "%s %d\n", T("vault.info.sessions"), len(all))
		}
		if !ok {
			return nil
		}
		if p, err := svc.People(cmd.Context()); err == nil {
			fmt.Fprintf(w, "%s %d\n", T("vault.info.classes"), len(p.Classes))
			fmt.Fprintf(w, "%s %d\n", T("vault.info.students"), len(p.List))
		}
		return nil
	})
	if list, _ := backup.List(v); len(list) > 0 {
		info, _ := os.Stat(list[0])
		when := ""
		if info != nil {
			when = info.ModTime().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "%s %s (%s)\n", T("vault.info.backup"), when, filepath.Base(list[0]))
	} else {
		fmt.Fprintf(w, "%s %s\n", T("vault.info.backup"), T("vault.info.no_backup"))
	}
	if !v.Encrypted() {
		fmt.Fprintln(w, "\n"+T("vault.info.plain_hint"))
	}
	return nil
}

// vaultState describes whether a vault is encrypted and open.
func vaultState(ctx context.Context, dataDir string, v *vault.Vault) string {
	switch {
	case !v.Encrypted():
		return T("vault.state.plain")
	case !v.Locked() || vault.AgentKey(ctx, dataDir, v.Meta.ID) != "":
		return T("vault.state.open")
	}
	return T("vault.state.locked")
}

// ---------------- init ----------------

func newVaultInitCmd() *cobra.Command {
	var name string
	var noEncrypt, use bool
	cmd := &cobra.Command{
		Use:     "init [folder]",
		Short:   T("vault.init.short"),
		Long:    T("vault.init.long"),
		Example: T("vault.init.example"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			target := vault.LocalDir
			if len(args) == 1 {
				target = args[0]
			}
			encrypt := cfg.Bool("vault.encrypt") && !noEncrypt
			pw := ""
			if encrypt {
				if !isTerminal() {
					return errors.New(T("vault.init.need_terminal"))
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("vault.init.password_intro"))
				if pw, err = askNewPassword(); err != nil {
					return err
				}
			}
			v, recovery, err := vault.Init(target, name, pw)
			if err != nil {
				return err
			}
			reg := loadRegistry(dir)
			v.Meta.Name = reg.Add(v.Meta.Name, v.Dir)
			_ = v.SetName(v.Meta.Name)
			if err := reg.Save(); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, T("vault.init.done", "name", v.Meta.Name, "dir", v.Dir))
			if encrypt {
				unlocked[v.Meta.ID] = v.Identity()
				showRecovery(cmd, recovery)
			} else {
				fmt.Fprintln(w, T("vault.init.plain"))
			}
			if use && filepath.Base(v.Dir) != vault.LocalDir {
				if err := config.Set(cfg.Path, "vault.path", v.Meta.Name); err != nil {
					return err
				}
				fmt.Fprintln(w, T("vault.use.done", "name", v.Meta.Name))
			} else if filepath.Base(v.Dir) == vault.LocalDir {
				fmt.Fprintln(w, T("vault.init.local_hint"))
			} else {
				fmt.Fprintln(w, T("vault.init.use_hint", "name", v.Meta.Name))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", T("vault.init.flag_name"))
	cmd.Flags().BoolVar(&noEncrypt, "no-encrypt", false, T("vault.init.flag_no_encrypt"))
	cmd.Flags().BoolVar(&use, "use", false, T("vault.init.flag_use"))
	return cmd
}

// ---------------- list / use / forget ----------------

func newVaultListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: T("vault.list.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			active, _, _ := activeVault(dir)
			reg := loadRegistry(dir)
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, T("vault.list.header"))
			seen := false
			for _, e := range reg.Vaults {
				mark := " "
				if active != nil && vault.SamePath(e.Path, active.Dir) {
					mark, seen = "*", true
				}
				state := T("vault.state.missing")
				if v, err := vault.Open(e.Path); err == nil {
					state = vaultState(cmd.Context(), dir, v)
				}
				fmt.Fprintf(tw, "%s %s\t%s\t%s\n", mark, e.Name, state, e.Path)
			}
			if active != nil && !seen {
				fmt.Fprintf(tw, "* %s\t%s\t%s\n", active.Meta.Name, vaultState(cmd.Context(), dir, active), active.Dir)
			}
			return tw.Flush()
		},
	}
}

func newVaultUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name|folder>",
		Short: T("vault.use.short"),
		Long:  T("vault.use.long"),
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			dir, _ := dataDir()
			var out []string
			for _, e := range loadRegistry(dir).Vaults {
				out = append(out, e.Name)
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			reg := loadRegistry(dir)
			name := args[0]
			e, ok := reg.Find(name)
			if !ok {
				p, _, err := vault.Locate(vault.Choice{Flag: name, DataDir: dir})
				if err != nil {
					return err
				}
				v, err := vault.Open(p)
				if err != nil {
					return err
				}
				e = vault.Entry{Name: reg.Add(v.Meta.Name, v.Dir), Path: v.Dir}
				if err := reg.Save(); err != nil {
					return err
				}
			}
			value := e.Name
			if vault.SamePath(e.Path, vault.DefaultDir(dir)) {
				value = "" // the default needs no setting
			}
			if value == "" {
				err = config.Unset(cfg.Path, "vault.path")
			} else {
				err = config.Set(cfg.Path, "vault.path", value)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.use.done", "name", e.Name))
			if s := cfg.Get("vault.path"); s.Source == config.FromEnv {
				fmt.Fprintln(cmd.ErrOrStderr(), T("vault.use.env_wins"))
			}
			return nil
		},
	}
}

func newVaultForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <name>",
		Short: T("vault.forget.short"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			reg := loadRegistry(dir)
			e, ok := reg.Find(args[0])
			if !ok || !reg.Forget(e.Name) {
				return errors.New(T("vault.forget.unknown", "name", args[0]))
			}
			if err := reg.Save(); err != nil {
				return err
			}
			if strings.EqualFold(cfg.Str("vault.path"), e.Name) {
				_ = config.Unset(cfg.Path, "vault.path")
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.forget.done", "name", e.Name, "dir", e.Path))
			return nil
		},
	}
}

// ---------------- lock / unlock ----------------

func newVaultLockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: T("vault.lock.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			lockVaults(cmd.Context(), dir)
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.lock.done"))
			return nil
		},
	}
}

func newVaultUnlockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock",
		Short: T("vault.unlock.short"),
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
			if !v.Encrypted() {
				fmt.Fprintln(cmd.OutOrStdout(), T("vault.state.plain"))
				return nil
			}
			if err := unlock(cmd.Context(), dir, v); err != nil {
				return err
			}
			if m := cfg.Int("vault.unlock_minutes"); m > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), T("vault.unlock.done", "n", m))
			}
			return nil
		},
	}
}

// ---------------- encrypt / decrypt / passwd ----------------

func newVaultEncryptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "encrypt",
		Short: T("vault.encrypt.short"),
		Long:  T("vault.encrypt.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if err := noSessionRunning(dir); err != nil {
				return err
			}
			v, _, err := activeVault(dir)
			if err != nil {
				return err
			}
			pw := ""
			if v.Encrypted() {
				if err := unlock(cmd.Context(), dir, v); err != nil { // finishing an interrupted run
					return err
				}
			} else {
				if !isTerminal() {
					return errors.New(T("vault.init.need_terminal"))
				}
				if pw, err = askNewPassword(); err != nil {
					return err
				}
			}
			db, err := openPlainDB(cmd.Context(), v)
			if err != nil {
				return err
			}
			defer db.Close()
			recovery, err := v.Encrypt(pw)
			if err != nil {
				return err
			}
			c, err := v.Cipher()
			if err != nil {
				return err
			}
			if err := db.Reseal(cmd.Context(), c); err != nil {
				return err
			}
			unlocked[v.Meta.ID] = v.Identity()
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.encrypt.done", "name", v.Meta.Name))
			if recovery != "" {
				showRecovery(cmd, recovery)
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.encrypt.backups_hint"))
			return nil
		},
	}
}

func newVaultDecryptCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "decrypt",
		Short: T("vault.decrypt.short"),
		Long:  T("vault.decrypt.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			if err := noSessionRunning(dir); err != nil {
				return err
			}
			v, _, err := activeVault(dir)
			if err != nil {
				return err
			}
			if !v.Encrypted() {
				fmt.Fprintln(cmd.OutOrStdout(), T("vault.state.plain"))
				return nil
			}
			if err := confirmPassword(v); err != nil {
				return err
			}
			if !yes && !confirm(cmd, T("vault.decrypt.confirm")) {
				return errors.New(T("cli.cancelled"))
			}
			db, err := openPlainDB(cmd.Context(), v)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.Reseal(cmd.Context(), nil); err != nil {
				return err
			}
			if err := v.Decrypt(); err != nil {
				return err
			}
			lockVaults(cmd.Context(), dir)
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.decrypt.done", "name", v.Meta.Name))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	return cmd
}

func newVaultPasswdCmd() *cobra.Command {
	var recovery bool
	cmd := &cobra.Command{
		Use:   "passwd",
		Short: T("vault.passwd.short"),
		Long:  T("vault.passwd.long"),
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
			if !v.Encrypted() {
				return errors.New(T("vault.passwd.plain"))
			}
			if !isTerminal() {
				return errors.New(T("vault.init.need_terminal"))
			}
			if recovery {
				key, err := readSecret(T("vault.passwd.recovery_prompt"))
				if err != nil {
					return err
				}
				v.Lock()
				if err := v.UnlockRecovery(key); err != nil {
					return err
				}
			} else if err := confirmPassword(v); err != nil {
				return err
			}
			pw, err := askNewPassword()
			if err != nil {
				return err
			}
			if err := v.SetPassword(pw); err != nil {
				return err
			}
			lockVaults(cmd.Context(), dir)
			fmt.Fprintln(cmd.OutOrStdout(), T("vault.passwd.done"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&recovery, "recovery", false, T("vault.passwd.flag_recovery"))
	return cmd
}

// ---------------- add / export ----------------

func newVaultAddCmd() *cobra.Command {
	var force, del, keep bool
	cmd := &cobra.Command{
		Use:     "add <exam-folder|file.examx>...",
		Short:   T("vault.add.short"),
		Long:    T("vault.add.long"),
		Example: T("vault.add.example"),
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			v, _, err := activeVault(dir)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			for _, src := range args {
				idx, folder, err := addToVault(v, src, force)
				if err != nil {
					return fmt.Errorf("%s: %w", src, err)
				}
				fmt.Fprintln(w, T("vault.add.done", "title", idx.Title, "id", idx.ExamID, "name", v.Meta.Name))
				if folder == "" || keep {
					continue
				}
				if del || (isTerminal() && confirm(cmd, T("vault.add.delete_original", "dir", folder))) {
					if err := os.RemoveAll(folder); err != nil {
						return err
					}
					fmt.Fprintln(w, T("vault.add.deleted", "dir", folder))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, T("vault.add.flag_force"))
	cmd.Flags().BoolVar(&del, "delete", false, T("vault.add.flag_delete"))
	cmd.Flags().BoolVar(&keep, "keep", false, T("vault.add.flag_keep"))
	return cmd
}

// addToVault stores a folder or an .examx file; it returns the folder
// when the source was one (to offer deleting it).
func addToVault(v *vault.Vault, src string, force bool) (*examx.Index, string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, "", err
	}
	if info.IsDir() {
		idx, err := storeExam(v, src, force)
		return idx, src, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, "", err
	}
	if examx.IsEncrypted(data) {
		if !isTerminal() {
			return nil, "", examx.ErrPassphrase
		}
		pass, err := readSecret(T("vault.add.passphrase"))
		if err != nil {
			return nil, "", err
		}
		if data, err = examx.Open(data, pass); err != nil {
			return nil, "", err
		}
	}
	c, err := examx.Read(data)
	if err != nil {
		return nil, "", err
	}
	probs, err := c.Validate()
	if err != nil {
		return nil, "", err
	}
	if probs.HasErrors() {
		return nil, "", errors.New(T("vault.add.invalid", "n", len(probs)))
	}
	file := examFile(c.Index.ExamID)
	if v.Exists(file) && !force {
		return nil, "", errors.New(T("vault.add.exists", "id", c.Index.ExamID))
	}
	return c.Index, "", v.WriteFile(file, data)
}

func newVaultExportCmd() *cobra.Command {
	var out, folder string
	var noKey, noEncrypt bool
	cmd := &cobra.Command{
		Use:     "export <exam>",
		Short:   T("vault.export.short"),
		Long:    T("vault.export.long"),
		Example: T("vault.export.example"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			v, _, err := activeVault(dir)
			if err != nil {
				return err
			}
			if err := confirmPassword(v); err != nil {
				return err
			}
			e, err := findVaultExam(v, args[0])
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if folder != "" {
				c, err := readExam(v, e.file)
				if err != nil {
					return err
				}
				if noKey {
					for name := range c.Files {
						if strings.HasPrefix(name, "private/") {
							delete(c.Files, name)
						}
					}
				}
				if _, err := c.Extract(folder, false); err != nil {
					return err
				}
				fmt.Fprintln(w, T("vault.export.folder_done", "dir", folder))
				return nil
			}
			tmp, cleanup, err := extractExam(v, e.file)
			if err != nil {
				return err
			}
			defer cleanup()
			data, idx, err := examx.Pack(tmp, examx.Options{NoKey: noKey})
			if err != nil {
				return err
			}
			if out == "" {
				out = vault.SafeName(idx.ExamID) + examx.Ext
			}
			if idx.HasKey && !noEncrypt {
				if !isTerminal() {
					return errors.New(T("vault.export.need_terminal"))
				}
				fmt.Fprintln(w, T("vault.export.passphrase_intro"))
				pass, err := askNewPassword()
				if err != nil {
					return err
				}
				if data, err = examx.Seal(data, pass); err != nil {
					return err
				}
			} else if idx.HasKey {
				fmt.Fprintln(cmd.ErrOrStderr(), T("vault.export.plain_warning"))
			}
			if _, err := os.Stat(out); err == nil {
				return errors.New(T("vault.export.exists", "file", out))
			}
			if err := os.WriteFile(out, data, 0o600); err != nil {
				return err
			}
			fmt.Fprintln(w, T("vault.export.done", "file", out))
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", T("vault.export.flag_out"))
	cmd.Flags().StringVar(&folder, "folder", "", T("vault.export.flag_folder"))
	cmd.Flags().BoolVar(&noKey, "no-key", false, T("vault.export.flag_no_key"))
	cmd.Flags().BoolVar(&noEncrypt, "no-encrypt", false, T("vault.export.flag_no_encrypt"))
	return cmd
}

// ---------------- agent (hidden) ----------------

func newVaultAgentCmd() *cobra.Command {
	var id string
	var idle time.Duration
	cmd := &cobra.Command{
		Use:    "agent",
		Short:  "Keep a vault open in memory (started by ax)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			return vault.RunAgent(cmd.Context(), dir, id, idle, os.Stdin, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "vault id")
	cmd.Flags().DurationVar(&idle, "idle", 15*time.Minute, "forget the key after this long unused")
	return cmd
}

// openPlainDB opens the vault's database with its current key, for
// resealing.
func openPlainDB(ctx context.Context, v *vault.Vault) (*sqlite.DB, error) {
	c, err := v.Cipher()
	if err != nil && !errors.Is(err, vault.ErrLocked) {
		return nil, err
	}
	return sqlite.Open(ctx, v.DB(), sqlite.WithCipher(c, false))
}
