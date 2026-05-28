package postfx

import (
	"math"
	"testing"
)

func TestReinhardMonotone(t *testing.T) {
	prev := float32(-1)
	for i := 0; i < 100; i++ {
		v := ApplyReinhard(float32(i) * 0.1)
		if v < prev {
			t.Fatalf("not monotone at %d: %v < %v", i, v, prev)
		}
		if v >= 1.0 {
			t.Fatalf("Reinhard exceeded 1: %v", v)
		}
		prev = v
	}
}

func TestExposure(t *testing.T) {
	got := ApplyExposure(1, 1)
	if math.Abs(float64(got)-2) > 1e-6 {
		t.Fatalf("ApplyExposure(1,1) = %v want 2", got)
	}
}
