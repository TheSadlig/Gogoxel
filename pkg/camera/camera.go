// Package camera re-exports the stable camera DTO from internal/platform.
// This keeps downstream code from depending on internal/.
package camera

import "Gogoxel/internal/platform"

// Camera is a yaw/pitch/FOV camera DTO. Zero-value is a camera at origin
// looking down +X with FOV 0 (caller should set FovDeg).
type Camera = platform.Camera
