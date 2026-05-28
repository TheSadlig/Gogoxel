package gi

import "testing"

func TestDefault(t *testing.T) {
	s := Default()
	if !s.AOEnabled || s.AORadius <= 0 || s.AOSamples <= 0 {
		t.Fatalf("default gi unreasonable: %+v", s)
	}
}
