package fluid

import "testing"

func TestGridAddressing(t *testing.T) {
	g := NewGrid(4, 4, 4)
	g.At(1, 2, 3).Level = 5
	if g.Cells[g.Index(1, 2, 3)].Level != 5 {
		t.Fatalf("addressing broken")
	}
}
