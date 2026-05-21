package world

import (
	"math"
	"math/bits"
)

const (
	rayHitEpsilon = float32(1e-4)

	axisMaskX = 1 << iota
	axisMaskY
	axisMaskZ
)

type Ray struct {
	Origin    [3]float32
	Direction [3]float32
}

type RaycastHit struct {
	Voxel      [3]uint32
	Normal     [3]int32
	MaterialID uint8
	Distance   float32
}

type rayBoxHit struct {
	tMin       float32
	tMax       float32
	normalMask int
}

type raycastChild struct {
	nodeIndex uint32
	origin    [3]uint32
	size      uint
	hit       rayBoxHit
}

func (s *SVO) Raycast(ray Ray, maxDistance float32) (RaycastHit, bool) {
	if s == nil || s.size == 0 || len(s.nodes) == 0 {
		return RaycastHit{}, false
	}
	if isZeroDirection(ray.Direction) {
		return RaycastHit{}, false
	}
	root := s.nodes[0]
	if len(s.nodes) == 1 && !root.isSolidLeaf() && !root.isBrickLeaf() && root.childMask() == 0 {
		return RaycastHit{}, false
	}
	if maxDistance <= 0 {
		maxDistance = math.MaxFloat32
	}

	rootHit, ok := intersectRayBox(ray, [3]float32{}, [3]float32{float32(s.size), float32(s.size), float32(s.size)}, 0, maxDistance)
	if !ok {
		return RaycastHit{}, false
	}
	return s.raycastNode(0, [3]uint32{}, s.size, ray, rootHit)
}

func (s *SVO) raycastNode(nodeIndex uint32, origin [3]uint32, size uint, ray Ray, boxHit rayBoxHit) (RaycastHit, bool) {
	if int(nodeIndex) >= len(s.nodes) {
		return RaycastHit{}, false
	}
	node := s.nodes[nodeIndex]
	if node.isBrickLeaf() {
		brick, ok := s.brickForNode(nodeIndex)
		if !ok {
			return RaycastHit{}, false
		}
		return raycastBrick(origin, *brick.Voxels, ray, boxHit)
	}
	if node.isSolidLeaf() {
		return raycastSolidLeaf(origin, size, node.materialID(), ray, boxHit), true
	}
	childMask := node.childMask()
	if childMask == 0 || node.childPointer == 0 || size <= 1 {
		return RaycastHit{}, false
	}

	childSize := size >> 1
	var children [8]raycastChild
	childCount := 0
	for octant := 0; octant < 8; octant++ {
		bit := uint8(1 << uint8(octant))
		if childMask&bit == 0 {
			continue
		}
		childIndex := node.childPointer + uint32(bits.OnesCount8(childMask&(bit-1)))
		childOrigin := origin
		childOrigin[0] += uint32(octant&1) * uint32(childSize)
		childOrigin[1] += uint32((octant>>1)&1) * uint32(childSize)
		childOrigin[2] += uint32((octant>>2)&1) * uint32(childSize)
		childMin := [3]float32{float32(childOrigin[0]), float32(childOrigin[1]), float32(childOrigin[2])}
		childMax := [3]float32{childMin[0] + float32(childSize), childMin[1] + float32(childSize), childMin[2] + float32(childSize)}
		childHit, ok := intersectRayBox(ray, childMin, childMax, maxFloat32(boxHit.tMin, 0), boxHit.tMax)
		if !ok {
			continue
		}
		candidate := raycastChild{
			nodeIndex: childIndex,
			origin:    childOrigin,
			size:      childSize,
			hit:       childHit,
		}
		insertIndex := childCount
		for insertIndex > 0 && raycastChildBefore(candidate, children[insertIndex-1]) {
			children[insertIndex] = children[insertIndex-1]
			insertIndex--
		}
		children[insertIndex] = candidate
		childCount++
	}
	if childCount == 0 {
		return RaycastHit{}, false
	}
	for childIndex := 0; childIndex < childCount; childIndex++ {
		child := children[childIndex]
		if hit, ok := s.raycastNode(child.nodeIndex, child.origin, child.size, ray, child.hit); ok {
			return hit, true
		}
	}
	return RaycastHit{}, false
}

func raycastChildBefore(candidate, existing raycastChild) bool {
	if candidate.hit.tMin == existing.hit.tMin {
		return candidate.nodeIndex < existing.nodeIndex
	}
	return candidate.hit.tMin < existing.hit.tMin
}

func raycastSolidLeaf(origin [3]uint32, size uint, materialID uint8, ray Ray, boxHit rayBoxHit) RaycastHit {
	distance := maxFloat32(boxHit.tMin, 0)
	point := advancePoint(ray, distance+rayHitEpsilon)
	voxel := pointToVoxel(point, origin, size)
	return RaycastHit{
		Voxel:      voxel,
		Normal:     resolveEntryNormal(boxHit.normalMask, ray.Direction),
		MaterialID: materialID,
		Distance:   distance,
	}
}

func raycastBrick(origin [3]uint32, voxels [BrickVoxelCount]uint8, ray Ray, boxHit rayBoxHit) (RaycastHit, bool) {
	currentT := maxFloat32(boxHit.tMin, 0)
	point := advancePoint(ray, currentT+rayHitEpsilon)
	originF := [3]float32{float32(origin[0]), float32(origin[1]), float32(origin[2])}
	voxel := [3]int{
		int(math.Floor(float64(clampFloat32(point[0]-originF[0], 0, float32(BrickSize)-rayHitEpsilon)))),
		int(math.Floor(float64(clampFloat32(point[1]-originF[1], 0, float32(BrickSize)-rayHitEpsilon)))),
		int(math.Floor(float64(clampFloat32(point[2]-originF[2], 0, float32(BrickSize)-rayHitEpsilon)))),
	}
	step := [3]int{signStep(ray.Direction[0]), signStep(ray.Direction[1]), signStep(ray.Direction[2])}
	tAxis := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	tDelta := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	for axis := 0; axis < 3; axis++ {
		direction := ray.Direction[axis]
		if step[axis] == 0 {
			continue
		}
		nextBoundary := float32(origin[axis] + uint32(voxel[axis]))
		if step[axis] > 0 {
			nextBoundary++
		}
		tAxis[axis] = (nextBoundary - ray.Origin[axis]) / direction
		tDelta[axis] = float32(math.Abs(float64(1 / direction)))
	}
	currentNormal := resolveEntryNormal(boxHit.normalMask, ray.Direction)

	for stepCount := 0; stepCount < BrickVoxelCount+3; stepCount++ {
		if voxel[0] < 0 || voxel[0] >= BrickSize || voxel[1] < 0 || voxel[1] >= BrickSize || voxel[2] < 0 || voxel[2] >= BrickSize {
			return RaycastHit{}, false
		}
		materialID := voxels[brickVoxelIndex(voxel[0], voxel[1], voxel[2])]
		if materialID != 0 {
			return RaycastHit{
				Voxel:      [3]uint32{origin[0] + uint32(voxel[0]), origin[1] + uint32(voxel[1]), origin[2] + uint32(voxel[2])},
				Normal:     currentNormal,
				MaterialID: materialID,
				Distance:   currentT,
			}, true
		}

		nextT := minFloat32(tAxis[0], minFloat32(tAxis[1], tAxis[2]))
		if nextT > boxHit.tMax {
			return RaycastHit{}, false
		}
		axisMask := 0
		if nearlyEqualFloat32(tAxis[0], nextT) {
			axisMask |= axisMaskX
		}
		if nearlyEqualFloat32(tAxis[1], nextT) {
			axisMask |= axisMaskY
		}
		if nearlyEqualFloat32(tAxis[2], nextT) {
			axisMask |= axisMaskZ
		}
		if axisMask&axisMaskX != 0 {
			voxel[0] += step[0]
			tAxis[0] += tDelta[0]
		}
		if axisMask&axisMaskY != 0 {
			voxel[1] += step[1]
			tAxis[1] += tDelta[1]
		}
		if axisMask&axisMaskZ != 0 {
			voxel[2] += step[2]
			tAxis[2] += tDelta[2]
		}
		currentT = nextT
		currentNormal = normalForAxisMask(axisMask, ray.Direction)
	}

	return RaycastHit{}, false
}

func intersectRayBox(ray Ray, boxMin, boxMax [3]float32, minDistance, maxDistance float32) (rayBoxHit, bool) {
	hit := rayBoxHit{tMin: minDistance, tMax: maxDistance}
	for axis := 0; axis < 3; axis++ {
		origin := ray.Origin[axis]
		direction := ray.Direction[axis]
		if nearlyZeroFloat32(direction) {
			if origin < boxMin[axis] || origin >= boxMax[axis] {
				return rayBoxHit{}, false
			}
			continue
		}
		invDirection := 1 / direction
		nearT := (boxMin[axis] - origin) * invDirection
		farT := (boxMax[axis] - origin) * invDirection
		nearMask := axisMaskForAxis(axis)
		if nearT > farT {
			nearT, farT = farT, nearT
		}
		if nearT > hit.tMin+rayHitEpsilon {
			hit.tMin = nearT
			hit.normalMask = nearMask
		} else if nearlyEqualFloat32(nearT, hit.tMin) {
			hit.normalMask |= nearMask
		}
		if farT < hit.tMax {
			hit.tMax = farT
		}
		if hit.tMin > hit.tMax {
			return rayBoxHit{}, false
		}
	}
	return hit, hit.tMax >= maxFloat32(hit.tMin, minDistance)
}

func pointToVoxel(point [3]float32, origin [3]uint32, size uint) [3]uint32 {
	maxCoord := float32(size) - rayHitEpsilon
	if maxCoord < 0 {
		maxCoord = 0
	}
	localX := clampFloat32(point[0]-float32(origin[0]), 0, maxCoord)
	localY := clampFloat32(point[1]-float32(origin[1]), 0, maxCoord)
	localZ := clampFloat32(point[2]-float32(origin[2]), 0, maxCoord)
	return [3]uint32{
		origin[0] + uint32(math.Floor(float64(localX))),
		origin[1] + uint32(math.Floor(float64(localY))),
		origin[2] + uint32(math.Floor(float64(localZ))),
	}
}

func advancePoint(ray Ray, distance float32) [3]float32 {
	return [3]float32{
		ray.Origin[0] + ray.Direction[0]*distance,
		ray.Origin[1] + ray.Direction[1]*distance,
		ray.Origin[2] + ray.Direction[2]*distance,
	}
}

func resolveEntryNormal(mask int, direction [3]float32) [3]int32 {
	if mask != 0 {
		return normalForAxisMask(mask, direction)
	}
	return dominantAxisNormal(direction)
}

func dominantAxisNormal(direction [3]float32) [3]int32 {
	axisMask := axisMaskX | axisMaskY | axisMaskZ
	return normalForAxisMask(axisMask, direction)
}

func normalForAxisMask(mask int, direction [3]float32) [3]int32 {
	axis := axisMaskX
	best := float32(-1)
	if mask&axisMaskX != 0 {
		best = absFloat32(direction[0])
		axis = axisMaskX
	}
	if mask&axisMaskY != 0 && absFloat32(direction[1]) >= best {
		best = absFloat32(direction[1])
		axis = axisMaskY
	}
	if mask&axisMaskZ != 0 && absFloat32(direction[2]) >= best {
		axis = axisMaskZ
	}
	result := [3]int32{}
	switch axis {
	case axisMaskX:
		if direction[0] > 0 {
			result[0] = -1
		} else {
			result[0] = 1
		}
	case axisMaskY:
		if direction[1] > 0 {
			result[1] = -1
		} else {
			result[1] = 1
		}
	case axisMaskZ:
		if direction[2] > 0 {
			result[2] = -1
		} else {
			result[2] = 1
		}
	}
	return result
}

func axisMaskForAxis(axis int) int {
	switch axis {
	case 0:
		return axisMaskX
	case 1:
		return axisMaskY
	case 2:
		return axisMaskZ
	default:
		return 0
	}
}

func nearlyEqualFloat32(a, b float32) bool {
	return absFloat32(a-b) <= rayHitEpsilon
}

func nearlyZeroFloat32(value float32) bool {
	return absFloat32(value) <= rayHitEpsilon
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func clampFloat32(value, minValue, maxValue float32) float32 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func signStep(value float32) int {
	if value > 0 {
		return 1
	}
	if value < 0 {
		return -1
	}
	return 0
}

func isZeroDirection(direction [3]float32) bool {
	return nearlyZeroFloat32(direction[0]) && nearlyZeroFloat32(direction[1]) && nearlyZeroFloat32(direction[2])
}
