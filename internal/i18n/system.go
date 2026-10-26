package i18n

import (
	"os"
	"strings"
)

// System guesses the user's language from the environment ($LC_ALL,
// $LC_MESSAGES, $LANG, or the Windows UI language), returning a code with
// a catalog, or Fallback.
func System() string {
	cands := []string{os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")}
	cands = append(cands, systemLanguages()...)
	for _, c := range cands {
		c, _, _ = strings.Cut(c, ".") // es_ES.UTF-8
		c = strings.ReplaceAll(c, "_", "-")
		if c == "" || c == "C" || c == "POSIX" {
			continue
		}
		if Has(c) {
			return c
		}
		if base, _, _ := strings.Cut(c, "-"); Has(strings.ToLower(base)) {
			return strings.ToLower(base)
		}
	}
	return Fallback
}
