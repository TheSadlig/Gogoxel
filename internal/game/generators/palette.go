package generators

func rgbaColor(red, green, blue uint8) uint32 {
	return uint32(red) | uint32(green)<<8 | uint32(blue)<<16 | 0xFF000000
}

func grayscaleColor(value uint8) uint32 {
	return rgbaColor(value, value, value)
}
