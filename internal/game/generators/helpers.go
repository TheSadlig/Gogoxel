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
