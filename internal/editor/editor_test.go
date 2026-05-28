package editor

import "testing"

type incOp struct{ v *int }

func (o incOp) Apply() { *o.v++ }
func (o incOp) Undo()  { *o.v-- }

func TestUndoRedo(t *testing.T) {
	var s Session
	v := 0
	s.Do(incOp{&v})
	s.Do(incOp{&v})
	if v != 2 {
		t.Fatalf("v=%d want 2", v)
	}
	s.Undo()
	if v != 1 {
		t.Fatalf("after undo v=%d want 1", v)
	}
	s.Redo()
	if v != 2 {
		t.Fatalf("after redo v=%d want 2", v)
	}
	s.Undo()
	s.Undo()
	if ok := s.Undo(); ok {
		t.Fatalf("expected empty undo")
	}
}
