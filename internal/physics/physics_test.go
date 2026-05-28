package physics

import (
	"math"
	"testing"
)

func TestStaticBodyDoesNotMove(t *testing.T) {
	b := Body{InverseMass: 0, Velocity: Vec3{1, 1, 1}}
	b.Step(1.0)
	if b.Position != (Vec3{}) {
		t.Fatalf("static body moved: %+v", b.Position)
	}
}

func TestGravityIntegration(t *testing.T) {
	w := World{Gravity: Vec3{0, -10, 0}, Bodies: []Body{{InverseMass: 1}}}
	for i := 0; i < 10; i++ {
		w.Tick(0.1)
	}
	got := w.Bodies[0].Position.Y
	if math.Abs(float64(got)-(-5.5)) > 1e-3 {
		t.Fatalf("falling body Y = %v want ~ -5.5", got)
	}
}
