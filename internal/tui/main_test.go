package tui

import (
	"os"
	"testing"
	"time"

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
	os.Exit(m.Run())
}

var testWizardDefaults = WizardDefaults{Language: "es", Duration: 45 * time.Minute, Visibility: "score", Profile: "standard",
	ShuffleQuestions: true, ShuffleOptions: true}
