// Package version exposes build metadata injected at link time.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set via -ldflags "-X github.com/lisk0vian/assess/internal/version.Version=..." by release builds.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// String returns a human-readable version line.
func String() string {
	commit, date := Commit, Date
	if commit == "" {
		commit, date = vcsInfo()
	}
	s := Version
	if commit != "" {
		s += " (" + commit
		if date != "" {
			s += ", " + date
		}
		s += ")"
	}
	return fmt.Sprintf("%s %s/%s", s, runtime.GOOS, runtime.GOARCH)
}

// vcsInfo falls back to the VCS stamp Go embeds in local builds.
func vcsInfo() (commit, date string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) > 12 {
				commit = s.Value[:12]
			} else {
				commit = s.Value
			}
		case "vcs.time":
			date = s.Value
		}
	}
	return commit, date
}
