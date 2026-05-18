package generators

import (
	"math"
	rand "math/rand/v2"
)

type perlinGenerator struct {
	perm [512]int
}

func NewPerlinGenerator(seed1, seed2 uint64) *perlinGenerator {
	values := make([]int, 256)
	for i := range values {
		values[i] = i
	}
	rng := rand.New(rand.NewPCG(seed1, seed2))
	for i := len(values) - 1; i > 0; i-- {
		j := rng.IntN(i + 1)
		values[i], values[j] = values[j], values[i]
	}
	g := &perlinGenerator{}
	for i := range g.perm {
		g.perm[i] = values[i&255]
	}
	return g
}

func (p *perlinGenerator) Generate(width, height, depth uint32) []uint32 {
	total := int(width * height * depth)
	data := make([]uint32, total)
	if width == 0 || height == 0 || depth == 0 {
		return data
	}

	baseNoise := p
	ridgeNoise := NewPerlinGenerator(3, 4)
	widthF := float64(width)
	heightF := float64(height)
	depthF := float64(depth)
	halfW := widthF * 0.5
	halfH := heightF * 0.5
	// vertical parameters
	baseHeight := 0
	heightScale := depthF * 0.95
	ridgeScale := depthF * 0.14
	minSurface := 0
	maxSurface := int(depth) - 1
	layerStride := width * height

	for y := uint32(0); y < height; y++ {
		for x := uint32(0); x < width; x++ {
			nx := (float64(x) - halfW) / widthF
			ny := (float64(y) - halfH) / heightF

			large := fbm2(baseNoise, nx*1.8, ny*1.8, 6, 2.0, 0.55)
			fine := fbm2(baseNoise, nx*18.0, ny*18.0, 4, 2.0, 0.5)
			elevation := math.Pow(large*0.6+fine*0.4, 1.15)

			mountainMask := fbm2(baseNoise, nx*0.55, ny*0.55, 3, 2.0, 0.7)
			ridgeRaw := fbm2(ridgeNoise, nx*8.0, ny*8.0, 4, 2.0, 0.65)
			ridge := math.Pow(math.Abs(ridgeRaw*2.0-1.0), 1.1)
			surface := baseHeight + int(elevation*heightScale) + int(mountainMask*depthF*0.35) - int(ridge*ridgeScale*0.6)
			if surface < minSurface {
				surface = minSurface
			}
			if surface > maxSurface {
				surface = maxSurface
			}

			columnOffset := y*width + x
			for z := uint32(0); z <= uint32(surface) && z < depth; z++ {
				depthFromSurface := surface - int(z)
				voxel := uint32(116)
				switch {
				case depthFromSurface == 0:
					voxel = 244
				case depthFromSurface < 4:
					voxel = 198
				case depthFromSurface < 18:
					voxel = 154
				}

				nz := float64(z) / depthF
				caveNoise := fbm3(baseNoise, nx*6.0, ny*6.0, nz*6.0, 4, 2.0, 0.6)
				caveRidge := fbm3(ridgeNoise, nx*12.0, ny*12.0, nz*8.0, 3, 2.0, 0.65)
				if caveNoise > 0.62 && caveRidge > 0.55 {
					// carve
				} else {
					data[int(z*layerStride+columnOffset)] = voxel
				}
			}
		}
	}

	return data
}

// ----- noise helpers (adapted from previous implementation) -----
func fbm2(noise *perlinGenerator, x, y float64, octaves int, lacunarity, gain float64) float64 {
	frequency := 1.0
	amplitude := 1.0
	total := 0.0
	totalAmplitude := 0.0
	for octave := 0; octave < octaves; octave++ {
		total += noise.noise2(x*frequency, y*frequency) * amplitude
		totalAmplitude += amplitude
		frequency *= lacunarity
		amplitude *= gain
	}
	if totalAmplitude == 0 {
		return 0
	}
	return total / totalAmplitude
}

func fbm3(noise *perlinGenerator, x, y, z float64, octaves int, lacunarity, gain float64) float64 {
	frequency := 1.0
	amplitude := 1.0
	total := 0.0
	totalAmplitude := 0.0
	for octave := 0; octave < octaves; octave++ {
		total += noise.noise3(x*frequency, y*frequency, z*frequency) * amplitude
		totalAmplitude += amplitude
		frequency *= lacunarity
		amplitude *= gain
	}
	if totalAmplitude == 0 {
		return 0
	}
	return total / totalAmplitude
}

func (p *perlinGenerator) noise2(x, y float64) float64 {
	xFloor := int(math.Floor(x))
	yFloor := int(math.Floor(y))
	xi := xFloor & 255
	yi := yFloor & 255
	xf := x - float64(xFloor)
	yf := y - float64(yFloor)
	u := fade(xf)
	v := fade(yf)
	aa := p.perm[p.perm[xi]+yi]
	ab := p.perm[p.perm[xi]+yi+1]
	ba := p.perm[p.perm[xi+1]+yi]
	bb := p.perm[p.perm[xi+1]+yi+1]
	x1 := lerp(grad2(aa, xf, yf), grad2(ba, xf-1, yf), u)
	x2 := lerp(grad2(ab, xf, yf-1), grad2(bb, xf-1, yf-1), u)
	return (lerp(x1, x2, v) + 1) * 0.5
}

func (p *perlinGenerator) noise3(x, y, z float64) float64 {
	xFloor := int(math.Floor(x))
	yFloor := int(math.Floor(y))
	zFloor := int(math.Floor(z))
	xi := xFloor & 255
	yi := yFloor & 255
	zi := zFloor & 255
	xf := x - float64(xFloor)
	yf := y - float64(yFloor)
	zf := z - float64(zFloor)
	u := fade(xf)
	v := fade(yf)
	w := fade(zf)
	aaa := p.perm[p.perm[p.perm[xi]+yi]+zi]
	aba := p.perm[p.perm[p.perm[xi]+yi+1]+zi]
	aab := p.perm[p.perm[p.perm[xi]+yi]+zi+1]
	abb := p.perm[p.perm[p.perm[xi]+yi+1]+zi+1]
	baa := p.perm[p.perm[p.perm[xi+1]+yi]+zi]
	bba := p.perm[p.perm[p.perm[xi+1]+yi+1]+zi]
	bab := p.perm[p.perm[p.perm[xi+1]+yi]+zi+1]
	bbb := p.perm[p.perm[p.perm[xi+1]+yi+1]+zi+1]
	x1 := lerp(grad3(aaa, xf, yf, zf), grad3(baa, xf-1, yf, zf), u)
	x2 := lerp(grad3(aba, xf, yf-1, zf), grad3(bba, xf-1, yf-1, zf), u)
	y1 := lerp(x1, x2, v)
	x3 := lerp(grad3(aab, xf, yf, zf-1), grad3(bab, xf-1, yf, zf-1), u)
	x4 := lerp(grad3(abb, xf, yf-1, zf-1), grad3(bbb, xf-1, yf-1, zf-1), u)
	y2 := lerp(x3, x4, v)
	return (lerp(y1, y2, w) + 1) * 0.5
}

func grad2(hash int, x, y float64) float64 {
	switch hash & 7 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x
	case 5:
		return -x
	case 6:
		return y
	default:
		return -y
	}
}

func grad3(hash int, x, y, z float64) float64 {
	switch hash & 15 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	case 3:
		return -x - y
	case 4:
		return x + z
	case 5:
		return -x + z
	case 6:
		return x - z
	case 7:
		return -x - z
	case 8:
		return y + z
	case 9:
		return -y + z
	case 10:
		return y - z
	case 11:
		return -y - z
	case 12:
		return x + y
	case 13:
		return -x + y
	case 14:
		return y + z
	default:
		return -y - z
	}
}

func fade(value float64) float64              { return value * value * value * (value*(value*6-15) + 10) }
func lerp(start, end, amount float64) float64 { return start + amount*(end-start) }
