package tui

import (
	"os"
	"testing"

	"github.com/lisk0vian/assess/internal/exam"
	"github.com/lisk0vian/assess/internal/i18n"
)

// The panel tests read Spanish screens; TestScreensInEnglish covers the
// default language.
func TestMain(m *testing.M) {
	if err := i18n.SetLanguage("es"); err != nil {
		panic(err)
	}
	exam.DefaultLanguage = "es"
	plainGlyphs = false // the screens are checked with their real glyphs, whatever the CI console
	os.Exit(m.Run())
}
