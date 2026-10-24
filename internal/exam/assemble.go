package exam

import "math/rand/v2"

// Slot is one question as a specific attempt sees it.
type Slot struct {
	QuestionID string   `json:"question_id"`
	Options    []string `json:"options,omitempty"` // option ids in display order
	Points     float64  `json:"points"`
}

// Assemble builds the deterministic question layout of one attempt: pool
// draws, question order and option order all derive from seed, so storing
// the seed is enough to reproduce exactly what a student saw.
//
// It assumes the exam is valid.
func (e *Exam) Assemble(seed uint64) []Slot {
	// Reproducibility is the point here; the seed itself comes from crypto/rand.
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) //nolint:gosec // G404: deterministic by design
	m := &e.Manifest

	var ids []string
	for _, it := range m.Items {
		switch {
		case it.Pool != nil:
			// Draw a subset, then keep the picks in their declared order so a
			// non-shuffled exam stays predictable.
			idx := rng.Perm(len(it.Pool.From))[:it.Pool.Pick]
			chosen := make([]bool, len(it.Pool.From))
			for _, i := range idx {
				chosen[i] = true
			}
			for i, id := range it.Pool.From {
				if chosen[i] {
					ids = append(ids, id)
				}
			}
		case it.Question != "":
			ids = append(ids, it.Question)
		}
	}
	if m.ShuffleQuestions() {
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	}

	slots := make([]Slot, len(ids))
	for i, id := range ids {
		q := e.Questions[id]
		slots[i] = Slot{QuestionID: id, Points: e.Key.Answers[id].Points}
		if len(q.Options) > 0 {
			slots[i].Options = orderOptions(q.Options, m.ShuffleOptions(), rng)
		}
	}
	return slots
}

// orderOptions shuffles unpinned options among the unpinned positions;
// pinned options keep their declared position.
func orderOptions(opts []Option, shuffle bool, rng *rand.Rand) []string {
	out := make([]string, len(opts))
	var free []int
	for i, o := range opts {
		out[i] = o.ID
		if !o.Pin {
			free = append(free, i)
		}
	}
	if shuffle {
		rng.Shuffle(len(free), func(a, b int) {
			out[free[a]], out[free[b]] = out[free[b]], out[free[a]]
		})
	}
	return out
}
