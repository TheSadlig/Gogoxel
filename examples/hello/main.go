// Package main is a "hello voxel" sample game stub that consumes the
// public pkg/ surface. See issue #8.
package main

import (
	"fmt"

	"Gogoxel/pkg/gogoxel"
	"Gogoxel/pkg/voxel"
)

func main() {
	fmt.Printf("hello from gogoxel sample (engine v%s)\n", gogoxel.Version)
	c := voxel.Coord{X: 1, Y: 2, Z: 3}
	fmt.Printf("brick voxel count = %d, sample coord = %+v\n", voxel.BrickVoxelCount, c)
}
