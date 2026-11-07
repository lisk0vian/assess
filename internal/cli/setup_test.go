package cli

import (
	"testing"

	"github.com/lisk0vian/assess/internal/vault"
)

// The welcome shows only where no vault exists yet.
func TestFirstVault(t *testing.T) {
	dir := t.TempDir()
	p, ok := firstVault(dir)
	if !ok || p != vault.DefaultDir(dir) {
		t.Fatalf("a new computer needs a vault: %q %v", p, ok)
	}
	testDB(t, dir)
	if _, ok := firstVault(dir); ok {
		t.Fatal("an existing vault skips the welcome")
	}
}
