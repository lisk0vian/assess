package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/config"
	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
)

// cfg is the teacher's settings. Execute loads it before building the
// command tree, so flag defaults and help come out of it; tests get the
// built-in defaults.
var cfg = config.Defaults()

var configFlag string

// loadSettings reads the settings file (named by --config in args, or
// $AX_CONFIG, or the default place), the translations next to it, and
// switches the language.
func loadSettings(args []string) {
	path, err := config.Path(configArg(args))
	if err != nil {
		cfg = config.Defaults()
		cfg.Problems = append(cfg.Problems, config.Problem{File: "config", Message: err.Error(), Warning: true})
		return
	}
	var localeProblems []config.Problem
	for _, err := range i18n.LoadDir(filepath.Join(filepath.Dir(path), "locales")) {
		localeProblems = append(localeProblems, config.Problem{File: "locales", Message: err.Error(), Warning: true})
	}
	c, err := config.Load(path)
	if err != nil {
		c = config.Defaults()
		c.Path = path
		c.Problems = append(c.Problems, config.Problem{File: path, Message: err.Error()})
	}
	c.Problems = append(c.Problems, localeProblems...)
	cfg = c
	applySettings()
	if configArg(args) != "" {
		_ = os.Setenv(config.EnvVar, path) // the exam server reads the same file
	}
}

// applySettings pushes the settings into the packages that use them.
func applySettings() {
	_ = i18n.SetLanguage(cfg.Language())
	exam.DefaultLanguage = cfg.StudentLanguage()
}

// configArg finds --config in the raw arguments.
func configArg(args []string) string {
	for i, a := range args {
		if a == "--" {
			break
		}
		if v, ok := strings.CutPrefix(a, "--config="); ok {
			return v
		}
		if a == "--config" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// checkSettings runs before every command: it reports problems in the
// settings file and writes the file on the first run. A broken file stops
// every command except `ax config`, which is how it gets fixed.
func checkSettings(cmd *cobra.Command) error {
	if cmd.Name() == "daemon" {
		return nil // the server logs; its parent already reported
	}
	errw := cmd.ErrOrStderr()
	for _, p := range cfg.Problems {
		if p.Warning {
			fmt.Fprintln(errw, p.Error())
		}
	}
	if cfg.HasErrors() && !underConfig(cmd) {
		for _, p := range cfg.Problems {
			if !p.Warning {
				fmt.Fprintln(errw, p.Error())
			}
		}
		fmt.Fprintln(errw, i18n.T("cli.config.fix_hint"))
		return errSilent
	}
	if !cfg.Exists && cfg.Path != "" {
		if cmd.Parent() == nil && isTerminal() && cfg.Get("language").Source == config.FromDefault {
			welcome()
		}
		if err := config.Create(cfg.Path, cfg.Language()); err == nil {
			cfg.Exists = true
		}
	}
	return nil
}

// welcome asks the interface language on the first run, preselecting the
// system's, and writes it to the new settings file.
func welcome() {
	lang := i18n.System()
	_ = i18n.SetLanguage(lang)
	var opts []huh.Option[string]
	for _, c := range i18n.Languages() {
		opts = append(opts, huh.NewOption(c.Name+" ("+c.Lang+")", c.Lang))
	}
	err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewNote().Title(T("welcome.title")).Description(T("welcome.body", "path", cfg.Path)),
		huh.NewSelect[string]().Title(T("welcome.language")).Options(opts...).Value(&lang),
	)))
	if err != nil {
		lang = i18n.Fallback
	}
	if err := config.Create(cfg.Path, lang); err != nil {
		return
	}
	if lang != config.Keys[0].Default {
		_ = config.Set(cfg.Path, "language", lang)
	}
	_ = reloadConfig()
}

func underConfig(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "config", "help", "version", "completion", "i18n":
			return true
		}
	}
	return false
}
