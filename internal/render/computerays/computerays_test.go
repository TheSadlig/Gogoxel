package computerays

import "testing"

func TestPlanCoversFull(t *testing.T) {
	tiles := Plan(100, 50, 32, 32, nil)
	var area int32
	for _, t := range tiles {
		area += t.W * t.H
	}
	if area != 100*50 {
		t.Fatalf("tile coverage wrong: %d want %d", area, 100*50)
	}
}
