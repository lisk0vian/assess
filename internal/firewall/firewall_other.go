//go:build !windows

package firewall

import "errors"

// ErrUnsupported is returned outside Windows: ax only manages its firewall.
var ErrUnsupported = errors.New("ax manages the firewall only on Windows; elsewhere ports above 1024 need no permission")

// Check reports that no firewall step is needed.
func Check(port int) *Status { return &Status{Supported: false, Port: port} }

// IsAdmin is false outside Windows.
func IsAdmin() bool { return false }

// Apply is not available outside Windows.
func Apply(Options, bool) error { return ErrUnsupported }

// Elevate is not available outside Windows.
func Elevate([]string) error { return ErrUnsupported }
