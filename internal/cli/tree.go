package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/session"
)

// The command tree reads `ax <thing> <action>`, and an action means the same
// on every thing (see verbs). The daily actions also stand alone as
// shortcuts, and every older name keeps working, hidden, so scripts and
// habits do not break.

// verbs are the only actions a thing may have, each with the words a
// teacher might type instead; a test keeps the tree to this list.
var verbs = map[string][]string{
	"list":     {"ls", "all", "listar", "lista", "ver-todo"},
	"show":     {"ver", "mostrar"},
	"new":      {"create", "init", "make", "crear", "nuevo", "nueva"},
	"edit":     {"modify", "change", "editar", "cambiar"},
	"rename":   {"mv", "renombrar"},
	"delete":   {"rm", "forget", "borrar", "eliminar", "olvidar"},
	"import":   {"load", "importar", "cargar"},
	"export":   {"save", "exportar", "guardar"},
	"add":      {"put", "enroll", "añadir", "anadir", "agregar", "meter", "asignar"},
	"remove":   {"unenroll", "quitar", "sacar"},
	"start":    {"run", "begin", "empezar", "iniciar", "comenzar"},
	"stop":     {"end", "finish", "parar", "terminar", "acabar"},
	"status":   {"state", "estado"},
	"open":     {"attach", "monitor", "panel", "abrir"},
	"resume":   {"continue", "reanudar", "seguir"},
	"logs":     {"log", "registro"},
	"report":   {"informe"},
	"results":  {"resultados"},
	"grades":   {"gradebook", "notas", "calificaciones"},
	"history":  {"historial"},
	"merge":    {"join", "fusionar", "unir"},
	"check":    {"validate", "verify", "validar", "comprobar", "revisar"},
	"guide":    {"format", "help-format", "formato", "guia", "guía"},
	"template": {"plantilla"},
	"use":      {"switch", "usar"},
	"lock":     {"bloquear", "cerrar"},
	"unlock":   {"desbloquear"},
	"password": {"passwd", "pass", "contraseña", "contrasena", "clave"},
	"encrypt":  {"cifrar"},
	"decrypt":  {"descifrar"},
	"restore":  {"restaurar", "recuperar"},
	"get":      {"leer"},
	"info":     {"explain", "explicar", "ayuda"}, // config: what a setting does
	"set":      {"poner", "cambiar"},
	"reset":    {"restablecer"},
	"path":     {"ruta", "donde"},
	"close":    {"cerrar"},
	"script":   {"guion"},
	// fixed parts of tools that are not things
	"agent": nil, "init": nil,
}

// things are the nouns, with the words a teacher might type instead.
var things = map[string][]string{
	"exam":     {"exams", "examen", "examenes", "exámenes", "test"},
	"session":  {"sessions", "sesion", "sesión", "sesiones"},
	"class":    {"classes", "group", "groups", "clase", "clases", "grupo", "grupos"},
	"student":  {"students", "alumno", "alumnos", "estudiante", "estudiantes"},
	"vault":    {"vaults", "boveda", "bóveda"},
	"backup":   {"backups", "copia", "respaldo"},
	"config":   {"settings", "ajustes", "configuracion", "configuración"},
	"firewall": {"cortafuegos"},
	"language": {"languages", "lang", "idioma", "idiomas"},
	"portable": {"usb"},
}

// as gives a command a new name, keeping its arguments, and the old one as an alias.
func as(name string, c *cobra.Command) *cobra.Command {
	old, args, _ := strings.Cut(c.Use, " ")
	c.Use = strings.TrimSpace(name + " " + args)
	if old != name {
		c.Aliases = append(c.Aliases, old)
	}
	return c
}

func hide(c *cobra.Command) *cobra.Command {
	c.Hidden = true
	return c
}

// strip takes a command's own subcommands away.
func strip(c *cobra.Command) *cobra.Command {
	c.RemoveCommand(c.Commands()...)
	return c
}

// child detaches one subcommand of c.
func child(c *cobra.Command, name string) *cobra.Command {
	for _, s := range c.Commands() {
		if s.Name() == name {
			c.RemoveCommand(s)
			return s
		}
	}
	panic("no subcommand " + name + " in " + c.Name())
}

// shortcut is a daily action at the top, marked with the full form.
func shortcut(full string, c *cobra.Command) *cobra.Command {
	c.Short += "  " + T("cli.shortcut_of", "cmd", "ax "+full)
	c.GroupID = "daily"
	return c
}

// thing makes a noun whose bare form lists, then names its actions.
func thing(name string, list func() *cobra.Command, actions ...*cobra.Command) *cobra.Command {
	c := &cobra.Command{
		Use:   name,
		Short: T("cli.thing." + name),
		Args:  unknownAction,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := runSub(cmd, list()); err != nil {
				return err
			}
			hintActions(cmd)
			return nil
		},
	}
	c.AddCommand(actions...)
	return c
}

// hintActions names what else a thing does, on stderr so a listing piped
// to a file stays clean.
func hintActions(cmd *cobra.Command) {
	var names []string
	for _, s := range cmd.Commands() {
		if s.IsAvailableCommand() {
			names = append(names, s.Name())
		}
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "\n"+T("cli.actions", "cmd", cmd.CommandPath(), "help", helpPath(cmd), "list", strings.Join(names, " · ")))
}

// helpPath is what follows `ax help` to reach cmd.
func helpPath(cmd *cobra.Command) string {
	return strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()))
}

// suggestions are the actions of cmd that word probably meant: a prefix, a
// typo of the name, or a typo of a word that points to it (borar → delete).
func suggestions(cmd *cobra.Command, word string) []string {
	w := strings.ToLower(word)
	var out []string
	for _, s := range cmd.Commands() {
		if !s.IsAvailableCommand() {
			continue
		}
		hit := len(w) >= 2 && strings.HasPrefix(s.Name(), w)
		for _, n := range append([]string{s.Name()}, s.SuggestFor...) {
			hit = hit || distance(w, strings.ToLower(n)) <= min(2, len([]rune(n))/4)
		}
		if hit {
			out = append(out, s.Name())
		}
	}
	return out
}

// distance is the Levenshtein distance between a and b, in runes.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	row := make([]int, len(rb)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		prev := row[0]
		row[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			prev, row[j] = row[j], min(row[j]+1, row[j-1]+1, prev+cost)
		}
	}
	return row[len(rb)]
}

// unknownAction refuses a word that is not an action of the command, and
// says which one was probably meant, in the teacher's language.
func unknownAction(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return unknownWord(cmd, args[0])
}

func unknownWord(cmd *cobra.Command, word string) error {
	msg := T("cli.unknown", "word", word, "cmd", cmd.CommandPath())
	if s := suggestions(cmd, word); len(s) > 0 {
		msg += "\n" + T("cli.did_you_mean", "list", cmd.CommandPath()+" "+strings.Join(s, " · "+cmd.CommandPath()+" "))
	}
	return errors.New(msg + "\n" + T("cli.see_help", "cmd", strings.TrimSpace("ax help "+helpPath(cmd))))
}

// withName lets a thing take a name (class 3B): bare it lists and names its
// actions; a name that is nobody's but looks like an action says so.
func withName(c *cobra.Command) *cobra.Command {
	run := c.RunE
	c.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return unknownWord(cmd, args[0])
		}
		return nil
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		switch {
		case err != nil && len(args) == 1 && len(suggestions(cmd, args[0])) > 0:
			return unknownWord(cmd, args[0])
		case err == nil && len(args) == 0:
			hintActions(cmd)
		}
		return err
	}
	return c
}

// suggest gives every command of the tree the words that point to it.
func suggest(c *cobra.Command) {
	words := verbs[c.Name()]
	if c.Parent() != nil && c.Parent().Parent() == nil && things[c.Name()] != nil {
		words = things[c.Name()]
	}
	c.SuggestFor = append(c.SuggestFor, words...)
	for _, s := range c.Commands() {
		suggest(s)
	}
}

func addTree(root *cobra.Command) {
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}

	// Daily shortcuts.
	add("daily", shortcut("exam new", newNewCmd()), shortcut("exam edit", newEditCmd()),
		shortcut("session start", newStartCmd()), shortcut("session stop", newStopCmd()),
		shortcut("session status", newStatusCmd()), newFindCmd(), newExploreCmd())

	// Things.
	exam := thing("exam", newExamsCmd, as("list", newExamsCmd()), newNewCmd(), newEditCmd(), as("check", newValidateCmd()),
		as("import", child(newVaultCmd(), "add")), as("export", child(newVaultCmd(), "export")), as("guide", newFormatCmd()))
	sess := thing("session", newSessionListCmd, newSessionListCmd(), newStartCmd(), newStopCmd(), newStatusCmd(),
		as("open", newAttachCmd()), newResumeCmd(), newLogsCmd(), newReportCmd(), newResultsCmd())

	class := withName(strip(newClassCmd()))
	show := as("show", strip(newClassCmd()))
	show.Use, show.Args, show.Short = "show <class>", cobra.ExactArgs(1), T("cli.show.class")
	class.AddCommand(newClassListCmd(), show, as("new", newClassCreateCmd()), newClassRenameCmd(), newClassDeleteCmd(),
		newClassImportCmd(), newClassExportCmd(), newClassTemplateCmd(), newClassAddCmd(), newClassRemoveCmd(), as("grades", newGradebookCmd()))

	student := withName(strip(newStudentCmd()))
	sshow := as("show", strip(newStudentCmd()))
	sshow.Use, sshow.Args, sshow.Short = "show <student>", cobra.ExactArgs(1), T("cli.show.student")
	student.AddCommand(newStudentListCmd(), sshow, as("new", newStudentAddCmd()), newStudentEditCmd(), newStudentRenameCmd(),
		newStudentMergeCmd(), newHistoryCmd(), as("delete", newStudentForgetCmd()))

	vault := newVaultCmd()
	vault.Args = unknownAction
	for _, s := range vault.Commands() {
		switch s.Name() {
		case "init":
			as("new", s)
		case "passwd":
			as("password", s)
		case "forget":
			as("remove", s)
		case "add", "export": // exams now: ax exam import / export
			hide(s)
		}
	}

	// Bare `ax backup` keeps making a backup: a scheduled task running it
	// must never quietly stop saving.
	bk := newBackupCmd()
	bk.Short, bk.Args = T("cli.thing.backup"), unknownAction
	bk.AddCommand(as("new", strip(newBackupCmd())), as("restore", newRestoreCmd()))

	for _, c := range []*cobra.Command{class, student, vault} {
		c.Short = T("cli.thing." + c.Name())
	}
	add("things", exam, sess, class, student, vault, bk)

	// Setup.
	config := newConfigCmd()
	config.AddCommand(newPanelPasswordCmd())
	fw := newFirewallCmd()
	fw.Args = unknownAction
	for _, s := range fw.Commands() {
		switch s.Name() {
		case "setup":
			as("open", s)
		case "remove":
			as("close", s)
		}
	}
	lang := as("language", newI18nCmd())
	add("setup", config, fw, lang, newPortableCmd(), newVersionCmd())

	// Older names, hidden: they still work exactly as before.
	for _, c := range []*cobra.Command{newExamsCmd(), newValidateCmd(), newFormatCmd(), newAttachCmd(), newResumeCmd(),
		newLogsCmd(), newResultsCmd(), newReportCmd(), newHistoryCmd(), newGradebookCmd(), newRestoreCmd(), newAdminCmd()} {
		root.AddCommand(hide(c))
	}
	root.AddCommand(newDaemonCmd())
	suggest(root)
}

// newPanelPasswordCmd sets or, with --clear, removes the teacher panel password.
func newPanelPasswordCmd() *cobra.Command {
	var clear bool
	admin := newAdminCmd()
	set, unset := child(admin, "set-password"), child(admin, "clear-password")
	cmd := &cobra.Command{
		Use:   "password",
		Short: T("cli.admin.short"),
		Long:  T("cli.admin.long"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clear {
				return unset.RunE(cmd, args)
			}
			return set.RunE(cmd, args)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, T("cli.admin.clear_short"))
	return cmd
}

// newSessionListCmd lists the sessions, newest first.
func newSessionListCmd() *cobra.Command {
	var f listFormat
	cmd := &cobra.Command{
		Use:   "list",
		Short: T("cli.session_list.short"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := dataDir()
			if err != nil {
				return err
			}
			return withStore(cmd.Context(), dir, func(s *store) error {
				all, err := s.svc.Sessions(cmd.Context())
				if err != nil {
					return err
				}
				if len(all) == 0 && !f.json && !f.csv {
					fmt.Fprintln(cmd.OutOrStdout(), T("explore.empty.sessions"))
					return nil
				}
				var recs [][]string
				for i := len(all) - 1; i >= 0; i-- {
					x := all[i]
					mean := ""
					if x.Submitted > 0 && x.State == session.StateEnded {
						mean = strconv.FormatFloat(round1(x.Average), 'f', -1, 64) + " %"
					}
					recs = append(recs, []string{x.CreatedAt.Local().Format("2006-01-02"), x.ExamTitle, x.ClassName, x.Code,
						T("state." + string(x.State)), strconv.Itoa(x.Submitted) + "/" + strconv.Itoa(x.Joined), mean})
				}
				return f.write(cmd.OutOrStdout(), all, strings.Split(T("cli.session_list.header"), ","), recs)
			})
		},
	}
	f.register(cmd)
	return cmd
}

// newClassTemplateCmd writes an empty class list to fill in.
func newClassTemplateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "template [file.csv]",
		Short:   T("explore.act.template"),
		Long:    T("people.template.format"),
		Args:    cobra.MaximumNArgs(1),
		Example: "ax class template\nax class template 3B.csv",
		RunE: func(cmd *cobra.Command, args []string) error {
			file := T("people.template.file")
			if len(args) == 1 {
				file = args[0]
			}
			if err := writeTemplate(file); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), T("people.template.saved_cli", "file", file))
			return nil
		},
	}
}
