package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lisk0vian/assess/internal/search"
	"github.com/lisk0vian/assess/internal/session"
	"github.com/lisk0vian/assess/internal/tui"
)

// pickLimit bounds the candidates offered by the picker.
const pickLimit = 30

// errCancelled is a picker closed without a choice.
var errCancelled = errors.New("cancelled")

// pickFn runs the picker; tests replace it.
var pickFn = tui.Pick

// choose resolves a typed reference that had no exact match: in a terminal
// the teacher picks among the candidates ("Did you mean…?"); otherwise the
// candidates are listed in the error.
func choose[E any](what, query string, hits []search.Hit[E], item func(E) tui.PickItem) (E, error) {
	var zero E
	if len(hits) == 0 {
		return zero, errors.New(T("pick.no_match", "what", T("pick.kind."+what), "q", query))
	}
	if len(hits) > pickLimit {
		hits = hits[:pickLimit]
	}
	if !isTerminal() {
		var lines []string
		for i, h := range hits {
			if i == 10 {
				lines = append(lines, "  …")
				break
			}
			it := item(h.Item)
			line := "  " + it.Key + "  " + it.Label
			if it.Detail != "" {
				line += "  (" + it.Detail + ")"
			}
			lines = append(lines, line)
		}
		return zero, fmt.Errorf("%s\n%s", T("pick.did_you_mean_list", "what", T("pick.kind."+what), "q", query), strings.Join(lines, "\n"))
	}
	items := make([]tui.PickItem, len(hits))
	for i, h := range hits {
		items[i] = item(h.Item)
	}
	i, err := pickFn(T("pick.did_you_mean", "q", query), items)
	if err != nil {
		return zero, err
	}
	if i < 0 {
		return zero, errCancelled
	}
	return hits[i].Item, nil
}

// studentItem shows a student in the picker.
func studentItem(p *session.People) func(*session.Student) tui.PickItem {
	return func(st *session.Student) tui.PickItem {
		return tui.PickItem{Key: st.Ref, Label: st.Name, Detail: strings.Join(p.ClassNames(st.ID), ", ")}
	}
}

// findStudent resolves a reference to a student of p: the identifier
// first, then anything that looks like it (name, class).
func findStudent(p *session.People, query string) (*session.Student, error) {
	if st := p.ByRef[session.NormalizeStudentID(query)]; st != nil {
		return st, nil
	}
	hits := search.Rank(query, p.List, func(st *session.Student) []string {
		return []string{st.Ref, st.Name, strings.Join(p.ClassNames(st.ID), " ")}
	})
	return choose("student", query, hits, studentItem(p))
}

// resolveStudent finds a student by identifier, or asks which one.
func resolveStudent(cmd *cobra.Command, svc *session.Service, query string) (*session.Student, *session.People, error) {
	p, err := svc.People(cmd.Context())
	if err != nil {
		return nil, nil, err
	}
	st, err := findStudent(p, query)
	return st, p, err
}

// findClass resolves a class name.
func findClass(p *session.People, query string) (*session.Class, error) {
	if c := p.Class(strings.TrimSpace(query)); c != nil {
		return c, nil
	}
	hits := search.Rank(query, p.Classes, func(c *session.Class) []string { return []string{c.Name} })
	return choose("class", query, hits, func(c *session.Class) tui.PickItem {
		return tui.PickItem{Key: c.Name, Label: T("people.n_students", "n", len(p.Members[c.ID]))}
	})
}

// resolveClass finds a class by name, or asks which one.
func resolveClass(cmd *cobra.Command, svc *session.Service, query string) (*session.Class, error) {
	p, err := svc.People(cmd.Context())
	if err != nil {
		return nil, err
	}
	return findClass(p, query)
}

// resolveSession finds a session by code or id; "" is the latest one.
func resolveSession(cmd *cobra.Command, svc *session.Service, query string) (*session.Overview, error) {
	all, err := svc.Sessions(cmd.Context())
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, errors.New(T("cli.no_sessions"))
	}
	if query == "" {
		return all[len(all)-1], nil
	}
	var exact []*session.Overview
	for _, s := range all {
		if s.ID == query || strings.EqualFold(s.Code, query) {
			exact = append(exact, s)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	reversed := make([]*session.Overview, len(all))
	for i, s := range all {
		reversed[len(all)-1-i] = s // newest first
	}
	hits := search.Rank(query, reversed, func(s *session.Overview) []string {
		return []string{s.Code, s.ExamTitle, s.ExamID, s.ClassName, s.CreatedAt.Local().Format("2006-01-02")}
	})
	return choose("session", query, hits, sessionItem)
}

func sessionItem(s *session.Overview) tui.PickItem {
	detail := s.CreatedAt.Local().Format("2006-01-02 15:04")
	if s.ClassName != "" {
		detail += " · " + s.ClassName
	}
	return tui.PickItem{Key: s.Code, Label: s.ExamTitle, Detail: detail}
}

// ---- completion ----

func completeStudents(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	var out []string
	dir, err := dataDir()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	_ = withLockedService(cmd.Context(), dir, func(svc *session.Service) error {
		p, err := svc.People(cmd.Context())
		if err != nil {
			return err
		}
		for _, st := range p.List {
			out = append(out, st.Ref+"\t"+st.Name)
		}
		return nil
	})
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeClasses(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	var out []string
	dir, err := dataDir()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	_ = withLockedService(cmd.Context(), dir, func(svc *session.Service) error {
		p, err := svc.People(cmd.Context())
		if err != nil {
			return err
		}
		for _, c := range p.Classes {
			out = append(out, c.Name)
		}
		return nil
	})
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeExams(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	dir, err := dataDir()
	if err != nil {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	v, _, err := activeVault(dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	return examIDs(v), cobra.ShellCompDirectiveFilterDirs
}
