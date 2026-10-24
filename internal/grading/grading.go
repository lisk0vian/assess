// Package grading validates student responses and scores them against the
// answer key. It runs only on the server.
package grading

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/lisk0vian/assess/internal/exam"
)

// MaxTextBytes bounds a short-answer response.
const MaxTextBytes = 500

// Response is what a student answered to one question. Exactly the field
// matching the question type is used.
type Response struct {
	Option  string   `json:"option,omitempty"`  // single
	Options []string `json:"options,omitempty"` // multiple
	Value   *bool    `json:"value,omitempty"`   // truefalse
	Text    string   `json:"text,omitempty"`    // short (number or text)
}

// Empty reports whether the response carries no answer (a cleared question).
func (r Response) Empty() bool {
	return r.Option == "" && len(r.Options) == 0 && r.Value == nil && strings.TrimSpace(r.Text) == ""
}

// ErrInvalidResponse is wrapped by Validate errors.
var ErrInvalidResponse = errors.New("invalid response")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidResponse, fmt.Sprintf(format, args...))
}

// Validate checks that r has the right shape for q. An empty response is
// valid: it clears the answer.
func Validate(q *exam.Question, r Response) error {
	if r.Empty() {
		return nil
	}
	has := func(id string) bool {
		return slices.ContainsFunc(q.Options, func(o exam.Option) bool { return o.ID == id })
	}
	switch q.Type {
	case exam.TypeSingle:
		if len(r.Options) > 0 || r.Value != nil || r.Text != "" {
			return invalid("single questions take one option")
		}
		if !has(r.Option) {
			return invalid("unknown option %q", r.Option)
		}
	case exam.TypeMultiple:
		if r.Option != "" || r.Value != nil || r.Text != "" {
			return invalid("multiple questions take a list of options")
		}
		seen := map[string]bool{}
		for _, id := range r.Options {
			if !has(id) {
				return invalid("unknown option %q", id)
			}
			if seen[id] {
				return invalid("duplicate option %q", id)
			}
			seen[id] = true
		}
	case exam.TypeTrueFalse:
		if r.Option != "" || len(r.Options) > 0 || r.Text != "" {
			return invalid("true/false questions take a value")
		}
	case exam.TypeShort:
		if r.Option != "" || len(r.Options) > 0 || r.Value != nil {
			return invalid("short questions take text")
		}
		if len(r.Text) > MaxTextBytes || !utf8.ValidString(r.Text) {
			return invalid("text is too long or not valid UTF-8")
		}
	default:
		return invalid("unsupported question type %q", q.Type)
	}
	return nil
}

// Result is the score of one response.
type Result struct {
	Points  float64 `json:"points"`
	Correct bool    `json:"correct"` // full marks
}

// Grade scores r for q with key entry a. Responses must have passed Validate.
func Grade(q *exam.Question, a exam.Answer, r Response) Result {
	if r.Empty() {
		return Result{}
	}
	full := Result{Points: a.Points, Correct: true}
	switch q.Type {
	case exam.TypeSingle:
		if r.Option == a.Option {
			return full
		}
	case exam.TypeMultiple:
		return gradeMultiple(a, r.Options)
	case exam.TypeTrueFalse:
		if a.Value != nil && *r.Value == *a.Value {
			return full
		}
	case exam.TypeShort:
		if q.Input == "number" {
			if n, ok := ParseNumber(r.Text); ok && a.Number != nil {
				tol := 0.0
				if a.Tolerance != nil {
					tol = *a.Tolerance
				}
				// A tiny epsilon absorbs float rounding in values like 0.1+0.2.
				if math.Abs(n-*a.Number) <= tol+1e-9 {
					return full
				}
			}
			return Result{}
		}
		given := Normalize(r.Text, a.Normalize)
		for _, acc := range a.Accept {
			if given == Normalize(acc, a.Normalize) {
				return full
			}
		}
	}
	return Result{}
}

// gradeMultiple is all-or-nothing, or with Partial:
// points × max(0, (correct_selected − wrong_selected) / total_correct).
func gradeMultiple(a exam.Answer, selected []string) Result {
	right, wrong := 0, 0
	for _, id := range selected {
		if slices.Contains(a.Options, id) {
			right++
		} else {
			wrong++
		}
	}
	total := len(a.Options)
	if total == 0 {
		return Result{}
	}
	if right == total && wrong == 0 {
		return Result{Points: a.Points, Correct: true}
	}
	if !a.Partial {
		return Result{}
	}
	frac := float64(right-wrong) / float64(total)
	if frac <= 0 {
		return Result{}
	}
	return Result{Points: roundPoints(a.Points * frac)}
}

func roundPoints(p float64) float64 { return math.Round(p*1e4) / 1e4 }

// ParseNumber accepts "4", " -2.5 ", "3,5" (decimal comma) and "1e3".
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ",") && !strings.Contains(s, ".") {
		s = strings.Replace(s, ",", ".", 1)
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// Normalize applies answer-key normalization steps in a fixed, safe order.
func Normalize(s string, steps []string) string {
	has := func(step string) bool { return slices.Contains(steps, step) }
	if has("strip_accents") {
		t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
		if out, _, err := transform.String(t, s); err == nil {
			s = out
		}
	}
	if has("casefold") {
		s = strings.ToLower(s)
	}
	if has("collapse_spaces") {
		s = strings.Join(strings.Fields(s), " ")
	}
	if has("trim") {
		s = strings.TrimSpace(s)
	}
	return s
}
