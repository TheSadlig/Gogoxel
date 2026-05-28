package material

import "testing"

func TestDefaultPalette(t *testing.T) {
	p := Default()
	if p[42].Roughness != 1 || p[42].IOR != 1.5 {
		t.Fatalf("default slot wrong: %+v", p[42])
	}
}
