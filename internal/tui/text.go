package tui

import (
	"math"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/lisk0vian/assess/internal/i18n"
)

// T translates an interface string; see locales/.
var T = i18n.T

// num writes v with one decimal at most, with the language's decimal mark.
func num(v float64) string {
	s := strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	return strings.Replace(s, ".", T("common.decimal"), 1)
}

func pct(v float64) string { return strconv.Itoa(int(math.Round(v))) + " %" }

func minutes(v float64) string { return T("common.minutes", "n", num(v)) }

func typeName(t string) string    { return known("qtype."+t, t) }
func stateName(s string) string   { return known("astate."+s, s) }
func signalName(s string) string  { return known("signal."+s, s) }
func penaltyName(s string) string { return known("penalty."+s, s) }

// known translates key, or returns fallback when no catalog has it.
func known(key, fallback string) string {
	if s := T(key); s != key {
		return s
	}
	return fallback
}

// yesNoKeys are the keys that answer yes and no: the first letters of the
// translated words, plus y/n which always work.
func yesNoKeys() (yes, no []string) {
	add := func(out []string, k string) []string {
		for _, s := range []string{strings.ToLower(k), strings.ToUpper(k)} {
			if s != "" && !contains(out, s) {
				out = append(out, s)
			}
		}
		return out
	}
	yes = add(add(nil, T("common.yes_key")), "y")
	no = add(add(nil, T("common.no_key")), "n")
	return yes, no
}

func isYes(k string) bool {
	yes, _ := yesNoKeys()
	return contains(yes, k)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// languageOptions lists the languages with a catalog, by their own names,
// keeping current even if this ax has no catalog for it.
func languageOptions(current string) []huh.Option[string] {
	var out []huh.Option[string]
	seen := false
	for _, c := range i18n.Languages() {
		name := c.Name
		if name == "" {
			name = c.Lang
		}
		out = append(out, huh.NewOption(name+" ("+c.Lang+")", c.Lang))
		seen = seen || c.Lang == current
	}
	if !seen && current != "" {
		out = append(out, huh.NewOption(current, current))
	}
	return out
}

func visibilityOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption(T("form.visibility.none"), "none"),
		huh.NewOption(T("form.visibility.score"), "score"),
		huh.NewOption(T("form.visibility.review"), "review"),
	}
}

func profileOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption(T("form.profile.standard"), "standard"),
		huh.NewOption(T("form.profile.strict"), "strict"),
		huh.NewOption(T("form.profile.off"), "off"),
	}
}

func readSpeedOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption(T("form.read_speed.slow"), "slow"),
		huh.NewOption(T("form.read_speed.normal"), "normal"),
		huh.NewOption(T("form.read_speed.fast"), "fast"),
	}
}
