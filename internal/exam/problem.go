package exam

import (
	"fmt"
	"sort"
	"strings"
)

// Severity of a validation problem.
type Severity string

// Severities. Errors block publication; warnings do not.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Problem is one validation finding, located as precisely as possible.
type Problem struct {
	Severity Severity `json:"severity"`
	File     string   `json:"file"`            // slash path relative to the exam folder
	Line     int      `json:"line,omitempty"`  // 1-based; 0 when unknown
	Field    string   `json:"field,omitempty"` // e.g. "items[3].pool.pick"
	Message  string   `json:"message"`
}

func (p Problem) String() string {
	var b strings.Builder
	b.WriteString(p.File)
	if p.Line > 0 {
		fmt.Fprintf(&b, ":%d", p.Line)
	}
	b.WriteString(": ")
	if p.Field != "" {
		b.WriteString(p.Field)
		b.WriteString(": ")
	}
	b.WriteString(p.Message)
	return b.String()
}

// Problems is a list of findings.
type Problems []Problem

// HasErrors reports whether any problem is an error.
func (ps Problems) HasErrors() bool { return ps.Count(SeverityError) > 0 }

// Count returns the number of problems with the given severity.
func (ps Problems) Count(s Severity) int {
	n := 0
	for _, p := range ps {
		if p.Severity == s {
			n++
		}
	}
	return n
}

// Sort orders problems by file, then line, then message, so output is stable.
func (ps Problems) Sort() {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.File != b.File {
			return fileRank(a.File) < fileRank(b.File) ||
				(fileRank(a.File) == fileRank(b.File) && a.File < b.File)
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Message < b.Message
	})
}

// fileRank lists the manifest first, then questions, the key and the rest.
func fileRank(f string) int {
	switch {
	case f == ManifestFile:
		return 0
	case strings.HasPrefix(f, QuestionsDir+"/"):
		return 1
	case f == AnswerKeyFile:
		return 2
	default:
		return 3
	}
}
