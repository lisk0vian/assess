// Package firewall checks and opens the student port in the operating
// system's firewall.
//
// On Windows a standard account cannot let a program accept connections:
// the first time ax listens, Windows asks for an administrator. Instead of
// a rule per program (which breaks when ax moves to another folder or a USB
// stick), ax opens its port range once, by port: whoever has the password
// does it once (`ax firewall setup`, which asks for elevation, or the script
// of `ax firewall script` through the school's IT), and every copy and
// version of ax works from then on.
package firewall

import (
	"fmt"
	"strings"

	"github.com/lisk0vian/assess/internal/netutil"
)

// RuleName names the rule in the firewall.
const RuleName = "Assess (ax)"

// Profile is a Windows network category.
type Profile string

// Network categories.
const (
	Public  Profile = "Public"
	Private Profile = "Private"
	Domain  Profile = "DomainAuthenticated"
)

// Network is one connected network.
type Network struct {
	Interface string
	Profile   Profile
}

// Status is what Check found.
type Status struct {
	Supported bool      // the system has a firewall ax knows how to check
	Port      int       // the server.port the check was made for
	Networks  []Network // connected networks and their category
	Rule      bool      // ax's port rule exists and is enabled
	AppRule   bool      // a rule lets this very copy of ax in (Windows' own, per folder)
	open      map[Profile]bool
	blocked   map[Profile]bool
	Admin     bool  // this process may change the firewall
	Err       error // the check itself failed
}

// Rule is one firewall rule as Windows stores it in the registry:
// "v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=6|Profile=Private|LPort=27337-27366|App=…|Name=…|".
type Rule struct {
	Name     string
	Allow    bool
	Active   bool
	In       bool
	Protocol string   // "6" is TCP; "" any
	Profiles []string // Domain, Private, Public; none means all
	Ports    []string // LPort values; none means all
	App      string
}

// ParseRule reads a rule from its registry form.
func ParseRule(v string) Rule {
	var r Rule
	for _, f := range strings.Split(v, "|") {
		k, val, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "Name":
			r.Name = val
		case "Action":
			r.Allow = strings.EqualFold(val, "Allow")
		case "Active":
			r.Active = strings.EqualFold(val, "TRUE")
		case "Dir":
			r.In = strings.EqualFold(val, "In")
		case "Protocol":
			r.Protocol = val
		case "Profile":
			r.Profiles = append(r.Profiles, val)
		case "LPort", "LPort2_10":
			r.Ports = append(r.Ports, strings.Split(val, ",")...)
		case "App":
			r.App = val
		}
	}
	return r
}

// coversPort reports whether the rule's ports include all of port's range.
func (r Rule) coversPort(port int) bool {
	if len(r.Ports) == 0 {
		return true
	}
	need := map[int]bool{}
	for p := port; p < port+netutil.PortRange; p++ {
		need[p] = true
	}
	for _, spec := range r.Ports {
		lo, hi, isRange := strings.Cut(spec, "-")
		var a, b int
		if _, err := fmt.Sscan(lo, &a); err != nil {
			continue
		}
		b = a
		if isRange {
			if _, err := fmt.Sscan(hi, &b); err != nil {
				continue
			}
		}
		for p := a; p <= b && p-a <= 65535; p++ {
			delete(need, p)
		}
	}
	return len(need) == 0
}

// profiles lists the categories a rule applies to.
func (r Rule) profiles() []Profile {
	if len(r.Profiles) == 0 {
		return []Profile{Domain, Private, Public}
	}
	var out []Profile
	for _, p := range r.Profiles {
		switch strings.ToLower(p) {
		case "domain":
			out = append(out, Domain)
		case "private":
			out = append(out, Private)
		case "public":
			out = append(out, Public)
		}
	}
	return out
}

// add takes one rule into account for the copy of ax at exe.
func (s *Status) add(r Rule, exe string) {
	if !r.Active || !r.In || (r.Protocol != "" && r.Protocol != "6") {
		return
	}
	forUs := false
	switch {
	case r.App != "":
		forUs = exe != "" && strings.EqualFold(r.App, exe)
	case r.Allow && r.Name == RuleName:
		forUs = r.coversPort(s.Port)
	case r.Allow:
		forUs = len(r.Ports) > 0 && r.coversPort(s.Port) // another port rule that happens to cover ours
	}
	if !forUs {
		return
	}
	if s.open == nil {
		s.open, s.blocked = map[Profile]bool{}, map[Profile]bool{}
	}
	for _, p := range r.profiles() {
		if r.Allow {
			s.open[p] = true
		} else {
			s.blocked[p] = true
		}
	}
	if r.Allow && r.App != "" {
		s.AppRule = true
	} else if r.Allow {
		s.Rule = true
	}
}

// Covers reports whether students on the network can reach ax.
func (s *Status) Covers(n Network) bool { return s.open[n.Profile] && !s.blocked[n.Profile] }

// Blocked reports whether a rule explicitly blocks ax on the network (a
// cancelled Windows prompt leaves one).
func (s *Status) Blocked(n Network) bool { return s.blocked[n.Profile] }

// Ready reports whether students can reach ax on every connected network.
func (s *Status) Ready() bool {
	if !s.Supported {
		return true
	}
	if len(s.Networks) == 0 {
		return s.Rule
	}
	for _, n := range s.Networks {
		if !s.Covers(n) {
			return false
		}
	}
	return true
}

// Ports is the range the rule opens for a base port: the port and the
// fallbacks the server may move to when it is busy.
func Ports(port int) string {
	return fmt.Sprintf("%d-%d", port, port+netutil.PortRange-1)
}

// Options shape the rule.
type Options struct {
	Port   int  // the server.port setting
	Public bool // also on networks Windows calls public
}

func (o Options) profiles() string {
	if o.Public {
		return "domain,private,public"
	}
	return "domain,private"
}

// addArgs and removeArgs are netsh's arguments; netsh keywords are the
// same in every Windows language.
func addArgs(o Options) []string {
	return []string{"advfirewall", "firewall", "add", "rule", "name=" + RuleName, "dir=in", "action=allow", "protocol=TCP",
		"localport=" + Ports(o.Port), "profile=" + o.profiles(),
		"description=Assess exams: students' browsers on the classroom network"}
}

func removeArgs() []string {
	return []string{"advfirewall", "firewall", "delete", "rule", "name=" + RuleName}
}

// line writes netsh arguments as a command line, quoting what has spaces.
func line(args []string) string {
	out := []string{"netsh"}
	for _, a := range args {
		if k, v, ok := strings.Cut(a, "="); ok && strings.ContainsAny(v, " '") {
			a = k + `="` + v + `"`
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}

// Commands are the lines that open the port, as written to the script.
func Commands(o Options) []string { return []string{line(removeArgs()), line(addArgs(o))} }

// RemoveCommand deletes the rule.
func RemoveCommand() string { return line(removeArgs()) }

// Script is a Windows batch file that opens the port, for IT or a group
// policy; readme explains it in the teacher's language.
func Script(o Options, readme string) string {
	var b strings.Builder
	b.WriteString("@echo off\r\nchcp 65001 >nul\r\n")
	for _, l := range strings.Split(strings.TrimSpace(readme), "\n") {
		b.WriteString("rem " + strings.TrimRight(l, "\r") + "\r\n")
	}
	b.WriteString("\r\n")
	for _, c := range Commands(o) {
		b.WriteString(c + "\r\n")
	}
	b.WriteString("pause\r\n")
	return b.String()
}
