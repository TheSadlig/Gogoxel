package asynccompute

import "testing"

func TestFIFO(t *testing.T) {
	var q Queue
	q.Push(Item{Kind: KindBrickUpload})
	q.Push(Item{Kind: KindSceneCopy})
	got := q.PopN(1)
	if len(got) != 1 || got[0].Kind != KindBrickUpload {
		t.Fatalf("FIFO violated: %+v", got)
	}
	if q.Len() != 1 {
		t.Fatalf("len = %d", q.Len())
	}
}
