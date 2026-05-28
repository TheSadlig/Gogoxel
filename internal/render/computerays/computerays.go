// Package computerays is the foundation of the compute-shader primary-
// ray path. See issue #17.
package computerays

// Tile is one work-group's screen-space tile.
type Tile struct {
	X, Y, W, H int32
}

// Plan partitions a (width x height) image into N x M tiles of size
// tileW x tileH (last tile in each row/column may be smaller).
func Plan(width, height, tileW, tileH int32, dst []Tile) []Tile {
	dst = dst[:0]
	for y := int32(0); y < height; y += tileH {
		h := tileH
		if y+h > height {
			h = height - y
		}
		for x := int32(0); x < width; x += tileW {
			w := tileW
			if x+w > width {
				w = width - x
			}
			dst = append(dst, Tile{X: x, Y: y, W: w, H: h})
		}
	}
	return dst
}
