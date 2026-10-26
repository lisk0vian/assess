// Package cli wires the ax command tree.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/daemon"
	"github.com/lisk0vian/assess/internal/i18n"
	"github.com/lisk0vian/assess/internal/version"
)

// T translates an interface string; see locales/.
var T = i18n.T

// Execute runs the root command and returns the process exit code.
func Execute() int {
	loadSettings(os.Args[1:])
	if err := newRootCmd().Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, T("cli.error_prefix"), err)
		}
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ax",
		Short:         T("cli.root.short"),
		Long:          T("cli.root.long"),
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return checkSettings(cmd)
		},
	}
	root.SetVersionTemplate("ax {{.Version}}\n")
	root.PersistentFlags().StringVar(&dataDirFlag, "data-dir", cfg.Str("data_dir"), T("cli.flag.data_dir"))
	root.PersistentFlags().StringVar(&configFlag, "config", "", T("cli.flag.config"))
	root.PersistentFlags().BoolP("help", "h", false, T("cli.flag.help"))
	root.Flags().BoolP("version", "v", false, T("cli.flag.version"))
	root.SetUsageTemplate(usageTemplate())
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [command]",
		Short: T("cli.help.short"),
		Run: func(c *cobra.Command, args []string) {
			cmd, _, e := c.Root().Find(args)
			if cmd == nil || e != nil {
				c.Printf(T("cli.help.unknown")+"\n", args)
				cmd = c.Root()
			}
			_ = cmd.Help()
		},
	})
	root.AddGroup(&cobra.Group{ID: "exam", Title: T("cli.group.exam")},
		&cobra.Group{ID: "session", Title: T("cli.group.session")},
		&cobra.Group{ID: "setup", Title: T("cli.group.setup")})
	root.SetCompletionCommandGroupID("setup")
	root.SetHelpCommandGroupID("setup")
	// Bare `ax` opens the panel of a running exam, or the menu in a
	// terminal, or shows help.
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		dir, err := dataDir()
		if err != nil {
			return cmd.Help()
		}
		if h, _, _ := daemon.Check(dir); h == daemon.Running {
			return attach(cmd, dir)
		}
		if isTerminal() {
			return home(cmd, dir)
		}
		return cmd.Help()
	}
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("exam", newNewCmd(), newEditCmd(), newValidateCmd(), newFormatCmd())
	add("session", newStartCmd(), newAttachCmd(), newStatusCmd(), newStopCmd(), newResumeCmd(), newLogsCmd(), newResultsCmd(), newReportCmd())
	add("setup", newConfigCmd(), newI18nCmd(), newAdminCmd(), newVersionCmd())
	root.AddCommand(newDaemonCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: T("cli.version.short"),
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "ax", version.String())
		},
	}
}

// usageTemplate is cobra's usage text with translated headings.
func usageTemplate() string {
	return T("cli.usage.usage") + `:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

` + T("cli.usage.aliases") + `:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

` + T("cli.usage.examples") + `:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

` + T("cli.usage.commands") + `:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}:{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

` + T("cli.usage.more_commands") + `:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + T("cli.usage.flags") + `:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

` + T("cli.usage.global_flags") + `:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

` + T("cli.usage.help_topics") + `:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

` + T("cli.usage.more_info") + `{{end}}
`
}
