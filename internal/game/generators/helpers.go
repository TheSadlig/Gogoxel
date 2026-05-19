package generators

type solidBounds struct {
	minX int
	maxX int
	minY int
	maxY int
	minZ int
	maxZ int
}

func (bounds solidBounds) valid() bool {
	return bounds.minX <= bounds.maxX && bounds.minY <= bounds.maxY && bounds.minZ <= bounds.maxZ
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}