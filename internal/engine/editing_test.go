package engine

import (
	"math"
	"testing"

	"Gogoxel/internal/platform"
)

func TestCursorRayMatchesRendererUVSpace(t *testing.T) {
	camera := platform.Camera{
		Position: [3]float32{12, 34, 56},
		YawDeg:   35,
		PitchDeg: -20,
		FovDeg:   60,
	}
	cursorCases := []struct {
		name string
		x    float32
		y    float32
	}{
		{name: "top-left", x: 0, y: 0},
		{name: "center", x: 0.5, y: 0.5},
		{name: "bottom-right", x: 1, y: 1},
	}

	for _, tc := range cursorCases {
		t.Run(tc.name, func(t *testing.T) {
			cursor := CursorSample{
				NormalizedX:    tc.x,
				NormalizedY:    tc.y,
				ViewportWidth:  1920,
				ViewportHeight: 1080,
			}
			ray, err := cursorRay(camera, cursor)
			if err != nil {
				t.Fatalf("cursorRay returned error: %v", err)
			}

			want := expectedRendererRayDirection(camera, cursor)
			assertVectorClose(t, ray.Direction, want, 1e-5)
			assertVectorClose(t, ray.Origin, camera.Position, 0)
		})
	}
}

func expectedRendererRayDirection(camera platform.Camera, cursor CursorSample) [3]float32 {
	screenX := cursor.NormalizedX*2 - 1
	screenY := cursor.NormalizedY*2 - 1
	aspect := float32(cursor.ViewportWidth) / float32(cursor.ViewportHeight)
	fovScale := float32(math.Tan(float64(camera.FovDeg) * 0.5 * math.Pi / 180.0))

	screenX *= aspect * fovScale
	screenY *= fovScale

	forward := camera.Forward()
	right := camera.Right()
	up := camera.Up()
	return normalize3([3]float32{
		forward[0] + right[0]*screenX + up[0]*screenY,
		forward[1] + right[1]*screenX + up[1]*screenY,
		forward[2] + right[2]*screenX + up[2]*screenY,
	})
}

func assertVectorClose(t *testing.T, got, want [3]float32, tolerance float64) {
	t.Helper()
	for axis := range got {
		if math.Abs(float64(got[axis]-want[axis])) > tolerance {
			t.Fatalf("axis %d mismatch: got %.6f want %.6f", axis, got[axis], want[axis])
		}
	}
}
