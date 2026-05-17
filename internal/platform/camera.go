package platform

import "math"

type Camera struct {
	Position [3]float32
	YawDeg   float32
	PitchDeg float32
	FovDeg   float32
}

func (c *Camera) Forward() [3]float32 {
	yaw := float32(c.YawDeg * math.Pi / 180.0)
	pitch := float32(c.PitchDeg * math.Pi / 180.0)
	x := float32(math.Cos(float64(pitch)) * math.Cos(float64(yaw)))
	y := float32(math.Cos(float64(pitch)) * math.Sin(float64(yaw)))
	z := float32(math.Sin(float64(pitch)))
	return [3]float32{x, y, z}
}

func (c *Camera) Right() [3]float32 {
	forward := c.Forward()
	rightX := -forward[1]
	rightY := forward[0]
	length := float32(math.Sqrt(float64(rightX*rightX + rightY*rightY)))
	if length == 0 {
		return [3]float32{0, 1, 0}
	}
	return [3]float32{rightX / length, rightY / length, 0}
}

func (c *Camera) Up() [3]float32 {
	forward := c.Forward()
	right := c.Right()
	upX := forward[1]*right[2] - forward[2]*right[1]
	upY := forward[2]*right[0] - forward[0]*right[2]
	upZ := forward[0]*right[1] - forward[1]*right[0]
	length := float32(math.Sqrt(float64(upX*upX + upY*upY + upZ*upZ)))
	if length == 0 {
		return [3]float32{0, 0, 1}
	}
	return [3]float32{-upX / length, -upY / length, -upZ / length}
}
