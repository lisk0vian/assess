package sqlite

import (
	"errors"
	"testing"

	"github.com/lisk0vian/assess/internal/session"
)

func TestCorrectionsRegradeAndUndo(t *testing.T) {
	e := newEnv(t)
	e.create()
	ana, leo := e.join("A1", "Ana"), e.join("L2", "Leo")
	if _, err := e.svc.Start(e.ctx, e.sess.ID); err != nil {
		t.Fatal(err)
	}
	var seq int64
	e.answerAll(ana, e.right, &seq)
	e.answerAll(leo, e.wrong, &seq)
	accept := session.Correction{QuestionID: "q001", Kind: session.CorrectAccept, Value: "a"}
	if _, err := e.svc.PreviewCorrection(e.ctx, e.sess.ID, accept); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("a running session is not corrected: %v", err)
	}
	for _, a := range []*session.Attempt{ana, leo} {
		if _, err := e.svc.Submit(e.ctx, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	score := func(id string) (float64, float64) {
		t.Helper()
		var s, m float64
		if err := e.db.View(e.ctx, func(tx session.Tx) error {
			a, err := tx.GetAttempt(id)
			s, m = a.Score, a.MaxScore
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return s, m
	}
	leoBefore, max0 := score(leo.ID)
	anaBefore, _ := score(ana.ID)

	// The preview names only who changes, and writes nothing.
	ch, err := e.svc.PreviewCorrection(e.ctx, e.sess.ID, accept)
	if err != nil || len(ch) != 1 || ch[0].Name != "Leo" || ch[0].After <= ch[0].Before {
		t.Fatalf("preview %+v %v", ch, err)
	}
	if s, _ := score(leo.ID); s != leoBefore {
		t.Fatal("the preview must not change grades")
	}

	c, err := e.svc.Correct(e.ctx, e.sess.ID, accept)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := score(leo.ID); s <= leoBefore {
		t.Fatalf("accepting a: %v → %v", leoBefore, s)
	}

	// Voiding a question takes it out of the maximum for everybody.
	if _, err := e.svc.Correct(e.ctx, e.sess.ID, session.Correction{QuestionID: "q003", Kind: session.CorrectVoid}); err != nil {
		t.Fatal(err)
	}
	if s, m := score(ana.ID); m >= max0 || s != m {
		t.Fatalf("Ana keeps a full grade on a smaller maximum: %v/%v (was %v/%v)", s, m, anaBefore, max0)
	}

	if err := e.svc.UndoCorrection(e.ctx, e.sess.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := e.svc.Corrections(e.ctx, e.sess.ID)
	if len(list) != 1 || list[0].Kind != session.CorrectVoid {
		t.Fatalf("one correction left: %+v", list)
	}
	if s, _ := score(leo.ID); s >= leoBefore+1e-9 && leoBefore > 0 {
		t.Fatalf("undo takes the accepted answer back: %v vs %v", s, leoBefore)
	}

	if _, err := e.svc.Correct(e.ctx, e.sess.ID, session.Correction{QuestionID: "q001", Kind: session.CorrectAccept, Value: "zz"}); !errors.Is(err, session.ErrInvalidInput) {
		t.Fatalf("an option that does not exist is refused: %v", err)
	}
}
