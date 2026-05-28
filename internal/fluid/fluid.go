// Package fluid is the foundation of the cellular-automaton water sim
// + rigid coupling + renderer. See issues #35, #36, #37. This first
// slice owns the per-cell DTO and a simple settle-down pass that is
// used by tests to validate determinism while higher-level slices land.
package fluid

// Cell is the per-voxel fluid state.
type Cell struct {
	Level   uint8 // 0..MaxLevel
	Settled bool
}

// MaxLevel is the highest fluid-level value a Cell may carry.
const MaxLevel uint8 = 8

// Grid is a flat row-major 3D buffer of Cells.
type Grid struct {
	W, H, D int
	Cells   []Cell
}

// NewGrid allocates a (w,h,d) grid.
func NewGrid(w, h, d int) *Grid {
	return &Grid{W: w, H: h, D: d, Cells: make([]Cell, w*h*d)}
}

// Index returns the flat index for (x,y,z).
func (g *Grid) Index(x, y, z int) int { return (z*g.H+y)*g.W + x }

// At returns a pointer to the Cell at (x,y,z).
func (g *Grid) At(x, y, z int) *Cell { return &g.Cells[g.Index(x, y, z)] }
