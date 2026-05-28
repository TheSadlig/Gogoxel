package taa

import (
	"math"
	"testing"
)

func TestHaltonInRange(t *testing.T) {
	for i := 0; i < 64; i++ {
		x, y := HaltonJitter(i)
		if math.Abs(float64(x)) >= 0.5 || math.Abs(float64(y)) >= 0.5 {
			t.Fatalf("frame %d jitter out of [-0.5,0.5): %v %v", i, x, y)
		}
	}
}
