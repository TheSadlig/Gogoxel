// Package frustum provides 6-plane view-frustum construction and a
// p-vertex AABB rejection test, intended to drive chunk-streamer priority
// biasing (issue #19).
//
// The implementation is allocation-free: Frustum is a value type, and
// ContainsAABB takes the AABB by value. Callers can reuse a single
// Frustum across many AABB tests.
package frustum

import (
	"math"

	"Gogoxel/internal/platform"
)

// Plane is a half-space ax + by + cz + d >= 0. Normal (a,b,c) points into
// the half-space we want to keep — i.e., a point P with N·P + d >= 0 is
// inside.
type Plane struct {
	N [3]float32
	D float32
}

// Frustum is a 6-plane view frustum.
//
// Plane order: left, right, bottom, top, near, far.
type Frustum struct {
	Planes [6]Plane
}

// FromCamera builds a view frustum from a platform.Camera given the
// viewport aspect ratio (width / height) and near/far distances. The
// returned planes have outward-pointing normals such that
// ContainsAABB returns true iff the AABB is at least partially inside.
//
// FovDeg is interpreted as the vertical field of view in degrees, matching
// the renderer's convention.
func FromCamera(camera platform.Camera, aspect, near, far float32) Frustum {
	forward := normalize3(cameraForward(camera))
	right := normalize3(cameraRight(camera))
	up := normalize3(cross(right, forward))

	halfV := float32(math.Tan(float64(camera.FovDeg) * math.Pi / 360.0))
	halfH := halfV * aspect

	pos := camera.Position
	farCenter := add3(pos, scale3(forward, far))

	// Frustum corner directions (from camera origin) used to derive side-plane normals.
	// Top/bottom: tilt forward by ±halfV up.
	// Left/right: tilt forward by ±halfH right.
	rightDir := normalize3(add3(scale3(forward, 1), scale3(right, halfH)))
	leftDir := normalize3(add3(scale3(forward, 1), scale3(right, -halfH)))
	topDir := normalize3(add3(scale3(forward, 1), scale3(up, halfV)))
	bottomDir := normalize3(add3(scale3(forward, 1), scale3(up, -halfV)))

	// Side-plane normals point INWARD (toward the frustum interior).
	// Derivations: each side plane contains the camera origin and the
	// corresponding far-plane edge. The inward normal is the cross product
	// of the camera "up" axis and the edge direction, with the sign chosen
	// per side so that points inside the frustum yield N·P + d ≥ 0.
	leftN := normalize3(cross(leftDir, up))
	rightN := normalize3(cross(up, rightDir))
	topN := normalize3(cross(topDir, right))
	bottomN := normalize3(cross(right, bottomDir))

	nearN := forward
	farN := scale3(forward, -1)

	planeFromPointNormal := func(point, normal [3]float32) Plane {
		return Plane{N: normal, D: -dot(normal, point)}
	}

	nearCenter := add3(pos, scale3(forward, near))

	return Frustum{Planes: [6]Plane{
		planeFromPointNormal(pos, leftN),
		planeFromPointNormal(pos, rightN),
		planeFromPointNormal(pos, bottomN),
		planeFromPointNormal(pos, topN),
		planeFromPointNormal(nearCenter, nearN),
		planeFromPointNormal(farCenter, farN),
	}}
}

// ContainsAABB returns true if the axis-aligned bounding box [min,max] is
// at least partially inside the frustum. False means the box is fully
// outside and may be safely culled.
//
// Implementation: classic "p-vertex" test. For each plane we pick the
// AABB corner most aligned with the plane normal; if even that corner is
// behind the plane then the whole box is outside.
func (f Frustum) ContainsAABB(min, max [3]float32) bool {
	for _, p := range f.Planes {
		// Pick the p-vertex (the AABB corner most aligned with the plane normal).
		pv := [3]float32{min[0], min[1], min[2]}
		if p.N[0] >= 0 {
			pv[0] = max[0]
		}
		if p.N[1] >= 0 {
			pv[1] = max[1]
		}
		if p.N[2] >= 0 {
			pv[2] = max[2]
		}
		if dot(p.N, pv)+p.D < 0 {
			return false
		}
	}
	return true
}

// --- internal helpers ---

func dot(a, b [3]float32) float32 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func add3(a, b [3]float32) [3]float32 { return [3]float32{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }

func scale3(a [3]float32, s float32) [3]float32 { return [3]float32{a[0] * s, a[1] * s, a[2] * s} }

func cross(a, b [3]float32) [3]float32 {
	return [3]float32{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func normalize3(v [3]float32) [3]float32 {
	l := float32(math.Sqrt(float64(dot(v, v))))
	if l == 0 {
		return v
	}
	return [3]float32{v[0] / l, v[1] / l, v[2] / l}
}

func cameraForward(c platform.Camera) [3]float32 { return c.Forward() }
func cameraRight(c platform.Camera) [3]float32   { return c.Right() }
