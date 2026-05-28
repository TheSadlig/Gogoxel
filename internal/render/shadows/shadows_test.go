package shadows

import "testing"

func TestDefaultSun(t *testing.T) {
	s := Default()
	if s.Intensity <= 0 || s.ShadowMaxDist <= 0 {
		t.Fatalf("default sun unreasonable: %+v", s)
	}
}
