// Package search matches what the teacher types against students, classes,
// exams and sessions the way a forgiving LIKE would: ignoring case, accents,
// spaces, dots and dashes, by substring, and tolerating a typo or two.
package search

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Fold lowercases s and removes accents and the separators people type
// differently ("A-12", "a 12", "a.12" all fold to "a12").
func Fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		out = s
	}
	var b strings.Builder
	for _, r := range strings.ToLower(out) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Words folds each word of s separately.
func Words(s string) []string {
	var out []string
	for _, w := range strings.Fields(strings.NewReplacer("-", " ", "_", " ", ".", " ", ",", " ").Replace(s)) {
		if f := Fold(w); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Score rates how well query matches the fields of a record: 0 is no
// match, higher is better. Exact matches beat prefixes, prefixes beat
// substrings, and substrings beat matches with typos.
func Score(query string, fields ...string) int {
	q := Fold(query)
	if q == "" {
		return 1 // everything matches an empty query
	}
	qw := Words(query)
	best := 0
	for i, f := range fields {
		weight := 0
		if i == 0 {
			weight = 5 // the first field is the identifier
		}
		ff := Fold(f)
		switch {
		case ff == "":
			continue
		case ff == q:
			best = max(best, 100+weight)
		case strings.HasPrefix(ff, q):
			best = max(best, 80+weight)
		case strings.Contains(ff, q):
			best = max(best, 60+weight)
		case allWords(qw, Words(f)):
			best = max(best, 50+weight)
		default:
			if s := typo(q, ff, Words(f)); s > 0 {
				best = max(best, s+weight)
			}
		}
	}
	return best
}

// allWords reports whether every query word starts some word of the field,
// so "gar ana" finds "Ana García".
func allWords(query, field []string) bool {
	if len(query) < 2 {
		return false
	}
	for _, q := range query {
		ok := false
		for _, f := range field {
			if strings.HasPrefix(f, q) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// typo scores a match within a small edit distance of the whole field or
// of one of its words.
func typo(q, whole string, words []string) int {
	allowed := 1
	if len([]rune(q)) >= 7 {
		allowed = 2
	}
	if len([]rune(q)) < 3 {
		return 0
	}
	best := 0
	try := func(target string) {
		if d := distance(q, target); d <= allowed {
			best = max(best, 40-10*d)
		}
		// A typo in a prefix: "garcai" for "garcia" inside a longer word.
		if rt := []rune(target); len(rt) > len([]rune(q)) {
			if d := distance(q, string(rt[:len([]rune(q))])); d <= allowed {
				best = max(best, 30-10*d)
			}
		}
	}
	try(whole)
	for _, w := range words {
		try(w)
	}
	return best
}

// distance is the Damerau–Levenshtein distance (adjacent swaps cost one).
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// Hit is a ranked match.
type Hit[T any] struct {
	Item  T
	Score int
}

// Rank returns the items that match query, best first; fields gives the
// searchable text of an item, its identifier first. Ties keep their order.
func Rank[T any](query string, items []T, fields func(T) []string) []Hit[T] {
	var out []Hit[T]
	for _, it := range items {
		if s := Score(query, fields(it)...); s > 0 {
			out = append(out, Hit[T]{Item: it, Score: s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
