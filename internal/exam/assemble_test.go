package exam

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func loadExample(t *testing.T) *Exam {
	t.Helper()
	ex, probs, err := Load(exampleDir)
	if err != nil || probs.HasErrors() {
		t.Fatalf("load example: %v %v", err, probs)
	}
	return ex
}

func TestAssembleDeterministic(t *testing.T) {
	ex := loadExample(t)
	a, b := ex.Assemble(42), ex.Assemble(42)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different layouts:\n%v\n%v", a, b)
	}
}

func TestAssembleRules(t *testing.T) {
	ex := loadExample(t)
	poolPicks := map[string]int{}
	orders := map[string]bool{}
	for seed := range uint64(200) {
		layout := ex.Assemble(seed)
		if len(layout) != 6 {
			t.Fatalf("seed %d: %d slots, want 6", seed, len(layout))
		}
		var ids []string
		inPool := 0
		for _, sl := range layout {
			ids = append(ids, sl.QuestionID)
			if slices.Contains([]string{"q004", "q005", "q007"}, sl.QuestionID) {
				inPool++
				poolPicks[sl.QuestionID]++
			}
			if sl.QuestionID == "q001" && sl.Options[len(sl.Options)-1] != "e" {
				t.Fatalf("seed %d: pinned option moved: %v", seed, sl.Options)
			}
			if want := ex.Key.Answers[sl.QuestionID].Points; sl.Points != want {
				t.Fatalf("seed %d: %s points %v, want %v", seed, sl.QuestionID, sl.Points, want)
			}
		}
		if inPool != 2 {
			t.Fatalf("seed %d: %d pool questions, want 2", seed, inPool)
		}
		for _, fixed := range []string{"q001", "q002", "q003", "q006"} {
			if !slices.Contains(ids, fixed) {
				t.Fatalf("seed %d: fixed question %s missing", seed, fixed)
			}
		}
		orders[ids[0]+ids[1]+ids[2]] = true
	}
	for _, id := range []string{"q004", "q005", "q007"} {
		if poolPicks[id] == 0 {
			t.Errorf("pool question %s was never drawn in 200 attempts", id)
		}
	}
	if len(orders) < 10 {
		t.Errorf("question order barely varies: %d distinct prefixes", len(orders))
	}
}

func TestAssembleWithoutShuffle(t *testing.T) {
	ex := loadExample(t)
	off := false
	ex.Manifest.Order = Order{ShuffleQuestions: &off, ShuffleOptions: &off}
	layout := ex.Assemble(7)
	var ids []string
	for _, sl := range layout {
		ids = append(ids, sl.QuestionID)
	}
	want := []string{"q001", "q002", "q003"}
	if !slices.Equal(ids[:3], want) || ids[5] != "q006" {
		t.Fatalf("order = %v, want fixed items in manifest order", ids)
	}
	if got := layout[0].Options; !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("options = %v, want declared order", got)
	}
}

func TestPublish(t *testing.T) {
	dir := copyExample(t)
	p1, _, err := Publish(dir)
	if err != nil {
		t.Fatal(err)
	}
	p2, _, err := Publish(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Hash != p2.Hash || len(p1.Hash) != 64 {
		t.Fatalf("hash not stable: %s vs %s", p1.Hash, p2.Hash)
	}
	if p1.Exam.Dir != "" {
		t.Fatal("published snapshot keeps the source folder")
	}

	applyEdit(t, dir, edit{file: "private/answer-key.yaml", old: "option: b", new: "option: c"})
	p3, _, err := Publish(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p3.Hash == p1.Hash {
		t.Fatal("changing the answer key did not change the hash")
	}

	writeFile(t, filepath.Join(dir, "assets", "d.png"), "png")
	p4, _, err := Publish(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p4.Hash == p3.Hash || string(p4.Assets["assets/d.png"]) != "png" {
		t.Fatal("assets are not part of the snapshot")
	}

	if err := os.Remove(filepath.Join(dir, "questions", "q003.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, probs, err := Publish(dir); !errors.Is(err, ErrInvalidExam) || !probs.HasErrors() {
		t.Fatalf("publishing an invalid exam: err = %v", err)
	}
}
