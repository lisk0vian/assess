// Package datadir locates ax's data directory: the SQLite database, the
// daemon state file and the logs.
package datadir

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// EnvVar overrides the default location.
const EnvVar = "AX_DATA_DIR"

// Resolve returns the data directory: flag, then $AX_DATA_DIR, then the OS
// default (%AppData%\assess, ~/Library/Application Support/assess,
// $XDG_DATA_HOME/assess or ~/.local/share/assess). It creates it if needed.
func Resolve(flag string) (string, error) {
	dir := flag
	if dir == "" {
		dir = os.Getenv(EnvVar)
	}
	if dir == "" {
		var err error
		if dir, err = defaultDir(); err != nil {
			return "", err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func defaultDir() (string, error) {
	switch runtime.GOOS {
	case "windows", "darwin":
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "assess"), nil
	default:
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "assess"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot find a home directory; pass --data-dir")
		}
		return filepath.Join(home, ".local", "share", "assess"), nil
	}
}

// DB is the SQLite database of a data directory.
func DB(dir string) string { return filepath.Join(dir, "assess.db") }

// State is the running daemon's state file (holds the control token).
func State(dir string) string { return filepath.Join(dir, "daemon.json") }

// Log is the daemon log file.
func Log(dir string) string { return filepath.Join(dir, "logs", "ax.log") }
