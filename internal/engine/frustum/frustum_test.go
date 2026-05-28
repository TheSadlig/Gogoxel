package frustum

import (
	"testing"

	"Gogoxel/internal/platform"
)

// Camera at origin looking down +X (yaw=0, pitch=0), 90° vertical FOV,
// aspect 1, near=1, far=100. The frustum opens along +X.
func axisAlignedFrustum() Frustum {
	return FromCamera(platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   90,
	}, 1, 1, 100)
}

func TestContainsAABBInsideFrustum(t *testing.T) {
	f := axisAlignedFrustum()
	// AABB centered along +X axis 10 units away — clearly inside.
	if !f.ContainsAABB([3]float32{9, -1, -1}, [3]float32{11, 1, 1}) {
		t.Fatal("expected centered AABB inside frustum to be contained")
	}
}

func TestContainsAABBBehindCamera(t *testing.T) {
	f := axisAlignedFrustum()
	// AABB behind the camera (negative X) — must be culled.
	if f.ContainsAABB([3]float32{-20, -1, -1}, [3]float32{-10, 1, 1}) {
		t.Fatal("expected AABB behind camera to be culled")
	}
}

func TestContainsAABBBeyondFar(t *testing.T) {
	f := axisAlignedFrustum()
	if f.ContainsAABB([3]float32{200, -1, -1}, [3]float32{210, 1, 1}) {
		t.Fatal("expected AABB beyond far plane to be culled")
	}
}

func TestContainsAABBOffToSide(t *testing.T) {
	f := axisAlignedFrustum()
	// 90° vertical FOV with aspect=1 → 90° horizontal FOV → half-angle 45°.
	// An AABB at x=10, y=100 is well outside the right plane.
	if f.ContainsAABB([3]float32{9, 100, -1}, [3]float32{11, 102, 1}) {
		t.Fatal("expected AABB far off to side to be culled")
	}
}

func TestContainsAABBStraddlingNear(t *testing.T) {
	f := axisAlignedFrustum()
	// AABB straddles the near plane (x∈[0.5, 2.5]). Some part inside → kept.
	if !f.ContainsAABB([3]float32{0.5, -0.5, -0.5}, [3]float32{2.5, 0.5, 0.5}) {
		t.Fatal("expected AABB straddling near plane to be kept")
	}
}

func TestContainsAABBYawedFrustum(t *testing.T) {
	// Look along +Y by yawing 90°.
	f := FromCamera(platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   90,
		PitchDeg: 0,
		FovDeg:   90,
	}, 1, 1, 100)
	if !f.ContainsAABB([3]float32{-1, 9, -1}, [3]float32{1, 11, 1}) {
		t.Fatal("expected AABB in front of yaw=90 camera to be kept")
	}
	if f.ContainsAABB([3]float32{-1, -11, -1}, [3]float32{1, -9, 1}) {
		t.Fatal("expected AABB behind yaw=90 camera to be culled")
	}
}

// BenchmarkContainsAABB measures the cost of a single AABB rejection test.
// Target from #19: < 0.05 ms for 256 chunks → ~200 ns/test budget.
func BenchmarkContainsAABB(b *testing.B) {
	f := axisAlignedFrustum()
	min := [3]float32{50, -1, -1}
	max := [3]float32{51, 1, 1}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = f.ContainsAABB(min, max)
	}
}
