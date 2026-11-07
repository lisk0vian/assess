package report

import (
	"slices"
	"time"
)

// Group is one class's session in a comparison of the same exam.
type Group struct {
	Name      string    `json:"name"`
	SessionID string    `json:"session_id"`
	Date      time.Time `json:"date"`
	Submitted int       `json:"submitted"`
	Mean      float64   `json:"mean"`   // percent of the maximum
	Median    float64   `json:"median"` // percent of the maximum
}

// QuestionGap is one question across the groups.
type QuestionGap struct {
	ID     string    `json:"id"`
	Prompt string    `json:"prompt"`
	Rates  []float64 `json:"rates"` // share right, 0 to 1, by group; -1 when the group never got it
	Gap    float64   `json:"gap"`   // highest rate minus lowest
}

// Comparison sets groups side by side, the questions where they differ most first.
type Comparison struct {
	ExamTitle string        `json:"exam_title"`
	Groups    []Group       `json:"groups"`
	Questions []QuestionGap `json:"questions"`
}

// Compare lines up the reports of one exam sat by different groups. names
// are the groups, in the order of reps.
func Compare(names []string, reps []*Report) *Comparison {
	c := &Comparison{}
	var order []string
	byQ := map[string]*QuestionGap{}
	for gi, r := range reps {
		if c.ExamTitle == "" {
			c.ExamTitle = r.Session.ExamTitle
		}
		c.Groups = append(c.Groups, Group{Name: names[gi], SessionID: r.Session.ID, Date: r.Session.StartedAt,
			Submitted: r.Summary.Submitted, Mean: r.Summary.Mean, Median: r.Summary.Median})
		for _, q := range r.Questions {
			g := byQ[q.ID]
			if g == nil {
				g = &QuestionGap{ID: q.ID, Prompt: q.Prompt, Rates: slices.Repeat([]float64{-1}, len(reps))}
				byQ[q.ID] = g
				order = append(order, q.ID)
			}
			if q.Seen > 0 {
				g.Rates[gi] = q.Rate
			}
		}
	}
	for _, id := range order {
		g := byQ[id]
		lo, hi, n := 1.0, 0.0, 0
		for _, r := range g.Rates {
			if r >= 0 {
				lo, hi, n = min(lo, r), max(hi, r), n+1
			}
		}
		if n >= 2 {
			g.Gap = hi - lo
		}
		c.Questions = append(c.Questions, *g)
	}
	slices.SortStableFunc(c.Questions, func(a, b QuestionGap) int {
		switch {
		case a.Gap > b.Gap:
			return -1
		case a.Gap < b.Gap:
			return 1
		}
		return 0
	})
	return c
}
