package grading

import (
	"errors"
	"testing"

	"github.com/lisk0vian/assess/internal/exam"
)

func ptr[T any](v T) *T { return &v }

var (
	single   = &exam.Question{ID: "q1", Type: exam.TypeSingle, Options: opts("a", "b", "c")}
	multiple = &exam.Question{ID: "q2", Type: exam.TypeMultiple, Options: opts("a", "b", "c", "d")}
	tf       = &exam.Question{ID: "q3", Type: exam.TypeTrueFalse}
	number   = &exam.Question{ID: "q4", Type: exam.TypeShort, Input: "number"}
	text     = &exam.Question{ID: "q5", Type: exam.TypeShort, Input: "text"}
)

func opts(ids ...string) []exam.Option {
	out := make([]exam.Option, len(ids))
	for i, id := range ids {
		out[i] = exam.Option{ID: id, Text: id}
	}
	return out
}

func TestGrade(t *testing.T) {
	multiKey := exam.Answer{Points: 2, Options: []string{"a", "b", "d"}}
	partialKey := multiKey
	partialKey.Partial = true
	numKey := exam.Answer{Points: 2, Number: ptr(4.0)}
	tolKey := exam.Answer{Points: 1, Number: ptr(3.14), Tolerance: ptr(0.01)}
	textKey := exam.Answer{Points: 1, Accept: []string{"Coeficiente"}, Normalize: []string{"trim", "casefold", "collapse_spaces", "strip_accents"}}

	cases := []struct {
		name string
		q    *exam.Question
		key  exam.Answer
		r    Response
		want Result
	}{
		{"single right", single, exam.Answer{Points: 1, Option: "b"}, Response{Option: "b"}, Result{1, true}},
		{"single wrong", single, exam.Answer{Points: 1, Option: "b"}, Response{Option: "a"}, Result{}},
		{"empty", single, exam.Answer{Points: 1, Option: "b"}, Response{}, Result{}},
		{"multiple exact", multiple, multiKey, Response{Options: []string{"d", "a", "b"}}, Result{2, true}},
		{"multiple missing one, no partial", multiple, multiKey, Response{Options: []string{"a", "b"}}, Result{}},
		{"multiple partial 2 of 3", multiple, partialKey, Response{Options: []string{"a", "b"}}, Result{1.3333, false}},
		{"multiple partial with a wrong one", multiple, partialKey, Response{Options: []string{"a", "b", "c"}}, Result{0.6667, false}},
		{"multiple one right one wrong cancels out", multiple, partialKey, Response{Options: []string{"a", "c"}}, Result{}},
		{"truefalse right", tf, exam.Answer{Points: 1, Value: ptr(false)}, Response{Value: ptr(false)}, Result{1, true}},
		{"truefalse wrong", tf, exam.Answer{Points: 1, Value: ptr(false)}, Response{Value: ptr(true)}, Result{}},
		{"number exact", number, numKey, Response{Text: " 4 "}, Result{2, true}},
		{"number as float", number, numKey, Response{Text: "4.0"}, Result{2, true}},
		{"number wrong", number, numKey, Response{Text: "5"}, Result{}},
		{"number garbage", number, numKey, Response{Text: "four"}, Result{}},
		{"number decimal comma within tolerance", number, tolKey, Response{Text: "3,145"}, Result{1, true}},
		{"number outside tolerance", number, tolKey, Response{Text: "3.2"}, Result{}},
		{"text normalized", text, textKey, Response{Text: "  COEFICIÉNTE "}, Result{1, true}},
		{"text wrong", text, textKey, Response{Text: "constante"}, Result{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.q, tc.r); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if got := Grade(tc.q, tc.key, tc.r); got != tc.want {
				t.Fatalf("Grade = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	long := make([]byte, MaxTextBytes+1)
	for i := range long {
		long[i] = 'x'
	}
	cases := []struct {
		name string
		q    *exam.Question
		r    Response
	}{
		{"unknown option", single, Response{Option: "z"}},
		{"list for single", single, Response{Options: []string{"a"}}},
		{"duplicate options", multiple, Response{Options: []string{"a", "a"}}},
		{"text for truefalse", tf, Response{Text: "true"}},
		{"option for short", number, Response{Option: "a"}},
		{"too long", text, Response{Text: string(long)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.q, tc.r); !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("Validate = %v, want ErrInvalidResponse", err)
			}
		})
	}
}

func TestParseNumber(t *testing.T) {
	for in, want := range map[string]float64{"4": 4, "-2.5": -2.5, "3,5": 3.5, "1e3": 1000, " 7 ": 7} {
		if got, ok := ParseNumber(in); !ok || got != want {
			t.Errorf("ParseNumber(%q) = %v, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "abc", "1,000.5x", "NaN", "Inf"} {
		if _, ok := ParseNumber(in); ok {
			t.Errorf("ParseNumber(%q) accepted", in)
		}
	}
}
