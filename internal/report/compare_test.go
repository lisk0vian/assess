package report

import "testing"

// The question where the classes differ most comes first; one a class never got has no rate.
func TestCompareOrdersByGap(t *testing.T) {
	a := &Report{Questions: []QuestionStat{{ID: "q1", Seen: 10, Rate: 0.9}, {ID: "q2", Seen: 10, Rate: 0.5}, {ID: "q3", Seen: 10, Rate: 0.7}}}
	b := &Report{Questions: []QuestionStat{{ID: "q1", Seen: 8, Rate: 0.85}, {ID: "q2", Seen: 8, Rate: 0.1}, {ID: "q3"}}}
	c := Compare([]string{"3B", "3C"}, []*Report{a, b})
	if len(c.Groups) != 2 || c.Questions[0].ID != "q2" || c.Questions[0].Gap < 0.39 || c.Questions[0].Gap > 0.41 {
		t.Fatalf("%+v", c.Questions)
	}
	for _, q := range c.Questions {
		if q.ID == "q3" && (q.Rates[1] != -1 || q.Gap != 0) {
			t.Fatalf("q3 was never seen by 3C: %+v", q)
		}
	}
}
