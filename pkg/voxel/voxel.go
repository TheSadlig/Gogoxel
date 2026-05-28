// Package voxel exports stable voxel-domain types: coordinates, brick
// constants, and palette layout. These are zero-value usable and have
// no cgo dependency.
package voxel

// Brick layout constants — mirror the values used by the internal SVO
// implementation. Changing these constants is a renderer-format break.
const (
	BrickSize       = 8
	BrickVoxelCount = BrickSize * BrickSize * BrickSize
	PaletteSize     = 255
)

// Coord is a voxel coordinate in world space (units of one voxel).
type Coord struct{ X, Y, Z int32 }

// Add returns c+o.
func (c Coord) Add(o Coord) Coord { return Coord{c.X + o.X, c.Y + o.Y, c.Z + o.Z} }
