package search

import "testing"

func TestFold(t *testing.T) {
	for in, want := range map[string]string{"A-12": "a12", "a 12": "a12", "García": "garcia", "Núñez.Ö": "nunezo"} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScore(t *testing.T) {
	cases := []struct {
		q, id, name string
		match       bool
	}{
		{"A-12", "A-12", "Ana García", true},
		{"a12", "A-12", "Ana García", true},
		{"garcia", "A-12", "Ana García", true},
		{"gar ana", "A-12", "Ana García", true},
		{"garcai", "A-12", "Ana García", true}, // swapped letters
		{"garsia", "A-12", "Ana García", true}, // one wrong letter
		{"lopez", "A-12", "Ana García", false},
		{"b7", "A-12", "Ana García", false},
	}
	for _, c := range cases {
		if got := Score(c.q, c.id, c.name) > 0; got != c.match {
			t.Errorf("Score(%q, %q, %q) match = %v, want %v", c.q, c.id, c.name, got, c.match)
		}
	}
	if Score("a12", "A-12") <= Score("a1", "A-12") {
		t.Error("an exact match must rank above a prefix")
	}
	if Score("ana", "B-1", "Ana") <= Score("ana", "B-2", "Mariana") {
		t.Error("a whole-word match must rank above a substring")
	}
}

func TestRank(t *testing.T) {
	type s struct{ id, name string }
	items := []s{{"B-7", "Mariana Ruiz"}, {"A-12", "Ana García"}, {"C-1", "Luis Pérez"}}
	hits := Rank("ana", items, func(x s) []string { return []string{x.id, x.name} })
	if len(hits) != 2 || hits[0].Item.id != "A-12" {
		t.Fatalf("Rank = %+v", hits)
	}
}
