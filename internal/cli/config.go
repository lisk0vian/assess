package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/tui"
)

func newConfigCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "config",
		Short:   T("cli.config.short"),
		Long:    T("cli.config.long"),
		Example: T("cli.config.example"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asJSON {
				return printConfigJSON(cmd.OutOrStdout())
			}
			printConfig(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, T("cli.flag.json"))

	keyArg := func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, k := range config.Keys {
			out = append(out, k.Name+"\t"+firstLine(config.Help(i18n.Language(), k)))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	valueArg := func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return keyArg(nil, args, "")
		}
		if len(args) > 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		k, ok := config.Lookup(args[0])
		switch {
		case !ok:
			return nil, cobra.ShellCompDirectiveNoFileComp
		case k.Kind == config.Choice:
			return k.Choices, cobra.ShellCompDirectiveNoFileComp
		case k.Kind == config.Bool:
			return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
		case k.Name == "language" || k.Name == "student_language":
			var langs []string
			for _, c := range i18n.Languages() {
				langs = append(langs, c.Lang+"\t"+c.Name)
			}
			return langs, cobra.ShellCompDirectiveNoFileComp
		case k.Name == "data_dir":
			return nil, cobra.ShellCompDirectiveFilterDirs
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	get := &cobra.Command{
		Use:               "get <key>",
		Short:             T("cli.config.get_short"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: keyArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := lookupKey(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), cfg.Get(k.Name).Raw)
			return nil
		},
	}
	set := &cobra.Command{
		Use:               "set <key> <value>",
		Short:             T("cli.config.set_short"),
		Example:           "  ax config set server.port 8080\n  ax config set language es\n  ax config set results.separator \";\"",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: valueArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := lookupKey(args[0])
			if err != nil {
				return err
			}
			if err := ensureConfigFile(); err != nil {
				return err
			}
			if err := config.Set(cfg.Path, k.Name, args[1]); err != nil {
				return err
			}
			if err := reloadConfig(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("cli.config.set_done", "key", k.Name, "value", cfg.Get(k.Name).Raw))
			if v := cfg.Get(k.Name); v.Source == config.FromEnv {
				fmt.Fprintln(cmd.OutOrStdout(), T("cli.config.env_wins", "env", k.Env()))
			}
			return nil
		},
	}
	var yes bool
	reset := &cobra.Command{
		Use:               "reset [key]",
		Short:             T("cli.config.reset_short"),
		Long:              T("cli.config.reset_long"),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: keyArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				k, err := lookupKey(args[0])
				if err != nil {
					return err
				}
				if err := config.Unset(cfg.Path, k.Name); err != nil {
					return err
				}
				if err := reloadConfig(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), T("cli.config.reset_key", "key", k.Name, "value", cfg.Get(k.Name).Raw))
				return nil
			}
			if !yes && !confirm(cmd, T("cli.config.reset_confirm")) {
				return errors.New(T("cli.cancelled"))
			}
			backup := cfg.Path + ".bak"
			if data, err := os.ReadFile(cfg.Path); err == nil {
				if err := os.WriteFile(backup, data, 0o600); err != nil {
					return err
				}
			}
			if err := os.Remove(cfg.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := config.Create(cfg.Path, cfg.Language()); err != nil {
				return err
			}
			if err := reloadConfig(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("cli.config.reset_all", "backup", backup))
			return nil
		},
	}
	reset.Flags().BoolVarP(&yes, "yes", "y", false, T("cli.flag.yes"))
	edit := &cobra.Command{
		Use:   "edit",
		Short: T("cli.config.edit_short"),
		Long:  T("cli.config.edit_long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ensureConfigFile(); err != nil {
				return err
			}
			for {
				c := tui.EditorCommand(cfg.Str("editor"), cfg.Path)
				c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
				if err := c.Run(); err != nil {
					return fmt.Errorf("%s: %w", c.Path, err)
				}
				if err := reloadConfig(); err != nil {
					return err
				}
				if !cfg.HasErrors() {
					fmt.Fprintln(cmd.OutOrStdout(), T("cli.config.edit_ok"))
					return nil
				}
				for _, p := range cfg.Problems {
					fmt.Fprintln(cmd.ErrOrStderr(), p.Error())
				}
				if !isTerminal() || !confirm(cmd, T("cli.config.edit_again")) {
					return errSilent
				}
			}
		},
	}
	path := &cobra.Command{
		Use:   "path",
		Short: T("cli.config.path_short"),
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), cfg.Path)
		},
	}
	info := &cobra.Command{
		Use:               "info <key>",
		Short:             T("cli.config.info_short"),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: keyArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := lookupKey(args[0])
			if err != nil {
				return err
			}
			printKeyInfo(cmd.OutOrStdout(), k)
			return nil
		},
	}
	cmd.AddCommand(get, set, reset, edit, path, info)
	return cmd
}

// editSettings opens the Settings screen and saves what changed.
func editSettings(cmd *cobra.Command) error {
	if err := ensureConfigFile(); err != nil {
		return err
	}
	s := tui.NewSettings(cfg)
	if _, err := tea.NewProgram(s).Run(); err != nil {
		return err
	}
	if len(s.Changes) == 0 {
		return nil
	}
	for _, ch := range s.Changes {
		var err error
		if ch.Reset {
			err = config.Unset(cfg.Path, ch.Key)
		} else {
			err = config.Set(cfg.Path, ch.Key, ch.Value)
		}
		if err != nil {
			return err
		}
	}
	if err := reloadConfig(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), T("cli.config.saved", "n", len(s.Changes), "path", cfg.Path))
	return nil
}

func lookupKey(name string) (config.Key, error) {
	k, ok := config.Lookup(name)
	if !ok {
		return k, errors.New(T("cli.config.unknown_key", "key", name))
	}
	return k, nil
}

func ensureConfigFile() error {
	if cfg.Path == "" {
		p, err := config.Path(configFlag)
		if err != nil {
			return err
		}
		cfg.Path = p
	}
	return config.Create(cfg.Path, cfg.Language())
}

// reloadConfig reads the file again after a change.
func reloadConfig() error {
	c, err := config.Load(cfg.Path)
	if err != nil {
		return err
	}
	cfg = c
	applySettings()
	return nil
}

func sourceText(v config.Value) string {
	switch v.Source {
	case config.FromFile:
		return T("cli.config.from_file", "line", v.Line)
	case config.FromEnv:
		return "$" + v.Key.Env()
	}
	return T("cli.config.from_default")
}

func printConfig(w io.Writer) {
	fmt.Fprintln(w, T("cli.config.file", "path", cfg.Path))
	if !cfg.Exists {
		fmt.Fprintln(w, T("cli.config.no_file"))
	}
	for _, p := range cfg.Problems {
		fmt.Fprintln(w, "  "+p.Error())
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	group := "-"
	for _, v := range cfg.All() {
		if g := v.Key.Group(); g != group {
			group = g
			fmt.Fprintln(tw, "\t\t")
		}
		val := v.Raw
		if val == "" {
			val = T("cli.config.empty")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Key.Name, val, sourceText(v))
	}
	_ = tw.Flush()
	fmt.Fprintln(w, "\n"+T("cli.config.hint"))
}

func printConfigJSON(w io.Writer) error {
	type row struct {
		Key     string   `json:"key"`
		Value   string   `json:"value"`
		Source  string   `json:"source"`
		Line    int      `json:"line,omitempty"`
		Default string   `json:"default"`
		Env     string   `json:"env"`
		Choices []string `json:"choices,omitempty"`
		Help    string   `json:"help"`
	}
	var rows []row
	for _, v := range cfg.All() {
		rows = append(rows, row{Key: v.Key.Name, Value: v.Raw, Source: string(v.Source), Line: v.Line,
			Default: v.Key.Default, Env: v.Key.Env(), Choices: v.Key.Choices, Help: i18n.T("config.key." + v.Key.Name)})
	}
	var problems []string
	for _, p := range cfg.Problems {
		problems = append(problems, p.Error())
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"path": cfg.Path, "exists": cfg.Exists, "settings": rows, "problems": problems})
}

func printKeyInfo(w io.Writer, k config.Key) {
	v := cfg.Get(k.Name)
	fmt.Fprintln(w, k.Name)
	fmt.Fprintln(w, "  "+strings.ReplaceAll(config.Help(i18n.Language(), k), "\n", "\n  "))
	fmt.Fprintln(w)
	def := k.Default
	if def == "" {
		def = T("cli.config.empty")
	}
	val := v.Raw
	if val == "" {
		val = T("cli.config.empty")
	}
	fmt.Fprintf(w, "  %-10s %s (%s)\n", T("cli.config.info_value"), val, sourceText(v))
	fmt.Fprintf(w, "  %-10s %s\n", T("cli.config.info_default"), def)
	fmt.Fprintf(w, "  %-10s %s\n", T("cli.config.info_env"), k.Env())
	fmt.Fprintf(w, "  %-10s ax config set %s <%s>\n", T("cli.config.info_change"), k.Name, T("cli.config.kind."+kindName(k.Kind)))
}

func kindName(k config.Kind) string {
	return [...]string{"text", "number", "bool", "duration", "choice"}[k]
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// ---------------- i18n ----------------

func newI18nCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "i18n",
		Short: T("cli.i18n.short"),
		Long:  T("cli.i18n.long"),
	}
	list := &cobra.Command{
		Use:   "list",
		Short: T("cli.i18n.list_short"),
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			total := len(i18n.Keys())
			for _, c := range i18n.Languages() {
				mark := " "
				if c.Lang == i18n.Language() {
					mark = "●"
				}
				fmt.Fprintf(tw, "%s %s\t%s\t%d%%\t%s\n", mark, c.Lang, c.Name, coverage(c, total), c.Source)
			}
			_ = tw.Flush()
		},
	}
	check := &cobra.Command{
		Use:   "check [language]",
		Short: T("cli.i18n.check_short"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			n := 0
			for _, p := range i18n.Check() {
				if len(args) == 1 && p.Lang != args[0] {
					continue
				}
				loc := p.Lang
				if p.Line > 0 {
					loc = fmt.Sprintf("%s:%d", p.Lang, p.Line)
				}
				fmt.Fprintf(out, "%s: %s: %s\n", loc, p.Key, p.Message)
				n++
			}
			if n > 0 {
				fmt.Fprintln(out, T("cli.i18n.problems", "n", n))
				return errSilent
			}
			fmt.Fprintln(out, T("cli.i18n.complete"))
			return nil
		},
	}
	newLang := &cobra.Command{
		Use:     "new <language>",
		Short:   T("cli.i18n.new_short"),
		Long:    T("cli.i18n.new_long"),
		Example: "  ax i18n new pt > ~/.config/assess/locales/pt.yaml",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !i18n.ValidLang(args[0]) {
				return errors.New(T("cli.new.bad_lang", "lang", args[0]))
			}
			_, err := cmd.OutOrStdout().Write(i18n.Template())
			return err
		},
	}
	cmd.AddCommand(list, check, newLang)
	return cmd
}

func coverage(c *i18n.Catalog, total int) int {
	if total == 0 {
		return 100
	}
	n := 0
	for _, k := range i18n.Keys() {
		if _, ok := c.Messages[k]; ok {
			n++
		}
	}
	return n * 100 / total
}
