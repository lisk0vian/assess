package cli

import (
	"slices"
	"strings"
	"testing"
)

// Every older name keeps working: scripts and habits do not break.
func TestOldCommandsStillWork(t *testing.T) {
	root := newRootCmd()
	for _, old := range []string{"exams", "validate", "format", "attach", "resume", "logs", "results", "report", "history",
		"gradebook", "restore", "admin set-password", "admin clear-password", "i18n list", "vault add", "vault export",
		"vault init", "vault passwd", "vault forget", "firewall setup", "firewall remove", "class create", "student add",
		"student forget", "backup", "backup list", "new", "edit", "start", "stop", "status", "find", "explore"} {
		c, rest, err := root.Find(strings.Fields(old))
		if err != nil || !c.Runnable() || len(rest) > 0 {
			t.Errorf("ax %s: %v (found %q, left %v)", old, err, c.CommandPath(), rest)
		}
	}
}

// Each thing uses only the verbs of the table, so an action means the same everywhere.
func TestThingsUseTheSameVerbs(t *testing.T) {
	for _, c := range newRootCmd().Commands() {
		if c.GroupID != "things" && c.GroupID != "setup" {
			continue
		}
		for _, a := range c.Commands() {
			if _, ok := verbs[a.Name()]; !ok && a.IsAvailableCommand() && a.Name() != "help" {
				t.Errorf("ax %s %s: not a verb of the table", c.Name(), a.Name())
			}
		}
	}
}

func TestMistypedCommandsSuggest(t *testing.T) {
	root := newRootCmd()
	for typed, want := range map[string]string{"clase": "class", "alumnos": "student", "class borar": "delete",
		"exam crear": "new", "session empezar": "start", "vault pasword": "password", "class plantilla": "template"} {
		words := strings.Fields(typed)
		c, _, _ := root.Find(words[:len(words)-1])
		if len(words) == 1 {
			c = root
		}
		if got := suggestions(c, words[len(words)-1]); !slices.Equal(got, []string{want}) {
			t.Errorf("ax %s suggests %v, want %s", typed, got, want)
		}
	}
	if _, err := run(t, "exam", "crear"); err == nil || !strings.Contains(err.Error(), "ax exam new") {
		t.Fatalf("the error names the right command: %v", err)
	}
}
