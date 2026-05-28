// Package gogoxel is the public façade of the Gogoxel voxel engine.
//
// It exposes a small, stable subset of the engine to downstream Go
// modules. Implementation details (Vulkan glue, brick streamer, file
// formats) remain in internal/ and are intentionally hidden from this
// surface — see issue #5 for the design rules.
//
// Status: v0.x, surface subject to breaking changes. See COMPATIBILITY.md
// once it lands.
package gogoxel

// Version is the public-API version of this module. It is a v0.x string
// while the surface stabilizes; consumers should treat any field /
// function rename in this layer as a SemVer-major change.
const Version = "0.1.0-dev"
