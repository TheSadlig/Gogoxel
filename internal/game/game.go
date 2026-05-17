package game

import (
	"fmt"
	"math"
	rand "math/rand/v2"
	"time"

	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/vulkan"

	"github.com/go-gl/glfw/v3.3/glfw"
)

const windowTitle = "Gogoxel Vulkan"

const moveCooldown = 120 * time.Millisecond

// Keep these in sync with the baked raytracer shader's chunk bounds.
const (
	chunkWidth  = uint32(1000)
	chunkHeight = uint32(1000)
	chunkDepth  = uint32(50)
)

const (
	actionMoveForward  input.Action = "move_forward"
	actionMoveBackward input.Action = "move_backward"
	actionMoveLeft     input.Action = "move_left"
	actionMoveRight    input.Action = "move_right"
	actionMoveAway     input.Action = "move_away"
	actionMoveCloser   input.Action = "move_closer"
	actionMoveUp       input.Action = "move_up"
	actionMoveDown     input.Action = "move_down"
	actionYawDown      input.Action = "yaw_down"
	actionYawUp        input.Action = "yaw_up"
	actionTurnRight    input.Action = "turn_right"
	actionTurnLeft     input.Action = "turn_left"
)

type Game struct {
	window   *platform.Window
	renderer *vulkan.Renderer
	input    *input.Manager
	elapsed  time.Duration

	fpsFrames  int
	fpsElapsed time.Duration
	lastFPS    float64
	camera     platform.Camera
	chunk      *vulkan.ChunkResources
	bindings   *vulkan.ChunkBindings
	chunkToTex *vulkan.ChunkToTexPipeline
	raytracer  *vulkan.RaytracerPipeline
}

type perlinNoise struct {
	perm [512]int
}

func New() *Game {
	return &Game{
		input: input.NewManager(defaultBindings()),
		camera: platform.Camera{
			Position: [3]float32{-140, 500, 290},
			YawDeg:   0,
			PitchDeg: 0,
			FovDeg:   60,
		},
	}
}

func (g *Game) InitChunk() error {
	if g.renderer == nil {
		return fmt.Errorf("renderer is not initialized")
	}
	if g.bindings == nil {
		return fmt.Errorf("chunk bindings are not initialized")
	}
	if g.chunkToTex == nil {
		return fmt.Errorf("chunktotex pipeline is not initialized")
	}

	if g.chunk != nil {
		g.chunk.Close()
		g.chunk = nil
	}

	data := generateChunkData(chunkWidth, chunkHeight, chunkDepth)

	chunk, err := g.renderer.CreateChunkResourcesFromData(g.bindings, data, chunkWidth, chunkHeight, chunkDepth)
	if err != nil {
		return err
	}

	if err := g.chunkToTex.DispatchOnce(g.renderer, chunk); err != nil {
		chunk.Close()
		return err
	}

	g.chunk = chunk
	return nil
}

func newPerlinNoise(seed1, seed2 uint64) perlinNoise {
	values := make([]int, 256)
	for index := range values {
		values[index] = index
	}

	rng := rand.New(rand.NewPCG(seed1, seed2))
	for index := len(values) - 1; index > 0; index-- {
		other := rng.IntN(index + 1)
		values[index], values[other] = values[other], values[index]
	}

	noise := perlinNoise{}
	for index := range noise.perm {
		noise.perm[index] = values[index&255]
	}

	return noise
}

func generateChunkData(width, height, depth uint32) []uint32 {
	total := int(width * height * depth)
	data := make([]uint32, total)
	if width == 0 || height == 0 || depth == 0 {
		return data
	}

	baseNoise := newPerlinNoise(1, 2)
	ridgeNoise := newPerlinNoise(3, 4)
	widthF := float64(width)
	heightF := float64(height)
	depthF := float64(depth)
	halfW := widthF * 0.5
	halfH := heightF * 0.5
	// Increase vertical relief: use most of the available depth for elevation
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

			// Combine large-scale and fine-scale FBM layers for richer variation
			large := fbm2(baseNoise, nx*1.8, ny*1.8, 6, 2.0, 0.55)
			fine := fbm2(baseNoise, nx*18.0, ny*18.0, 4, 2.0, 0.5)
			elevation := math.Pow(large*0.6+fine*0.4, 1.15)

			// Low-frequency mountain mask to create high peaks
			mountainMask := fbm2(baseNoise, nx*0.55, ny*0.55, 3, 2.0, 0.7)

			// Sharper ridges used to cut valleys; reduce overall subtraction
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

				// Carve caves/overhangs using 3D FBM: if noise is high, make empty
				// Map coordinates to a normalized 3D noise space
				nz := float64(z) / depthF
				caveNoise := fbm3(baseNoise, nx*6.0, ny*6.0, nz*6.0, 4, 2.0, 0.6)
				// Slightly different ridged noise for sharper tunnels
				caveRidge := fbm3(ridgeNoise, nx*12.0, ny*12.0, nz*8.0, 3, 2.0, 0.65)
				if caveNoise > 0.62 && caveRidge > 0.55 {
					// leave as empty (zero)
				} else {
					data[int(z*layerStride+columnOffset)] = voxel
				}
			}
		}
	}

	return data
}

func fbm2(noise perlinNoise, x, y float64, octaves int, lacunarity, gain float64) float64 {
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

func (p perlinNoise) noise2(x, y float64) float64 {
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

func (p perlinNoise) noise3(x, y, z float64) float64 {
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

func fbm3(noise perlinNoise, x, y, z float64, octaves int, lacunarity, gain float64) float64 {
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

func fade(value float64) float64 {
	return value * value * value * (value*(value*6-15) + 10)
}

func lerp(start, end, amount float64) float64 {
	return start + amount*(end-start)
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

func (g *Game) Run() error {
	window, err := platform.NewWindow(windowTitle, vulkan.DefaultWindowWidth, vulkan.DefaultWindowHeight)
	if err != nil {
		return err
	}
	defer window.Close()

	renderer, err := vulkan.New(window)
	if err != nil {
		return err
	}
	defer renderer.Close()

	bindings, err := renderer.NewChunkBindings()
	if err != nil {
		return err
	}
	defer bindings.Close()

	chunkToTex, err := renderer.NewChunkToTexPipeline(bindings)
	if err != nil {
		return err
	}
	defer chunkToTex.Close()

	raytracer, err := renderer.NewRaytracerPipeline(bindings)
	if err != nil {
		return err
	}
	defer raytracer.Close()

	g.window = window
	g.renderer = renderer
	g.bindings = bindings
	g.chunkToTex = chunkToTex
	g.raytracer = raytracer
	if err := g.InitChunk(); err != nil {
		return err
	}
	defer g.chunk.Close()
	g.updateWindowTitle()
	lastFrame := time.Now()

	for !window.ShouldClose() {
		window.PollEvents()
		if window.IsIconified() {
			lastFrame = time.Now()
			continue
		}

		now := time.Now()
		delta := now.Sub(lastFrame)
		lastFrame = now

		if err := g.Update(delta); err != nil {
			return err
		}
		if err := renderer.DrawFrame(g.Render); err != nil {
			return err
		}
		g.recordFrame(delta)
	}

	return renderer.WaitIdle()
}

func (g *Game) Update(delta time.Duration) error {
	g.elapsed += delta
	g.input.Update(g.window, time.Now())

	const moveUnitsPerSecond = float32(6)
	const turnDegreesPerSecond = float32(120)
	const maxPitch = float32(89)
	deltaSeconds := float32(delta.Seconds())
	moveStep := moveUnitsPerSecond * deltaSeconds
	turnStep := turnDegreesPerSecond * deltaSeconds

	forward := g.camera.Forward()
	walkForward := [3]float32{forward[0], forward[1], 0}
	walkForwardLength := float32(math.Sqrt(float64(walkForward[0]*walkForward[0] + walkForward[1]*walkForward[1])))
	if walkForwardLength > 0 {
		walkForward[0] /= walkForwardLength
		walkForward[1] /= walkForwardLength
	}
	right := g.camera.Right()

	if g.input.Down(actionMoveForward) {
		g.camera.Position[0] += walkForward[0] * moveStep
		g.camera.Position[1] += walkForward[1] * moveStep
	}
	if g.input.Down(actionMoveBackward) {
		g.camera.Position[0] -= walkForward[0] * moveStep
		g.camera.Position[1] -= walkForward[1] * moveStep
	}
	if g.input.Down(actionMoveLeft) {
		g.camera.Position[0] -= right[0] * moveStep
		g.camera.Position[1] -= right[1] * moveStep
	}
	if g.input.Down(actionMoveRight) {
		g.camera.Position[0] += right[0] * moveStep
		g.camera.Position[1] += right[1] * moveStep
	}

	if g.input.Down(actionMoveAway) {
		g.camera.Position[0] -= forward[0] * moveStep
		g.camera.Position[1] -= forward[1] * moveStep
		g.camera.Position[2] -= forward[2] * moveStep
	}
	if g.input.Down(actionMoveCloser) {
		g.camera.Position[0] += forward[0] * moveStep
		g.camera.Position[1] += forward[1] * moveStep
		g.camera.Position[2] += forward[2] * moveStep
	}

	// vertical movement (Q/E)
	if g.input.Down(actionMoveUp) {
		g.camera.Position[2] += moveStep
	}
	if g.input.Down(actionMoveDown) {
		g.camera.Position[2] -= moveStep
	}
	if g.input.Down(actionYawDown) {
		g.camera.YawDeg -= turnStep
	}
	if g.input.Down(actionYawUp) {
		g.camera.YawDeg += turnStep
	}
	if g.input.Down(actionTurnRight) {
		g.camera.PitchDeg -= turnStep
	}
	if g.input.Down(actionTurnLeft) {
		g.camera.PitchDeg += turnStep
	}

	if g.camera.PitchDeg > maxPitch {
		g.camera.PitchDeg = maxPitch
	}
	if g.camera.PitchDeg < -maxPitch {
		g.camera.PitchDeg = -maxPitch
	}

	return nil
}

func (g *Game) recordFrame(delta time.Duration) {
	g.fpsFrames++
	g.fpsElapsed += delta

	if g.fpsElapsed < time.Second {
		return
	}

	g.lastFPS = float64(g.fpsFrames) / g.fpsElapsed.Seconds()
	g.fpsFrames = 0
	g.fpsElapsed = 0
	g.updateWindowTitle()
}

func (g *Game) updateWindowTitle() {
	if g.renderer == nil {
		return
	}

	title := windowTitle
	if g.lastFPS > 0 {
		title = fmt.Sprintf("%s | %.1f FPS", windowTitle, g.lastFPS)
	}

	g.window.SetTitle(title)
}

func (g *Game) Render(frame *vulkan.Frame) error {
	if g.raytracer == nil {
		return fmt.Errorf("raytracer pipeline is not initialized")
	}
	if g.chunk == nil {
		return fmt.Errorf("chunk resources are not initialized")
	}

	return g.raytracer.Record(frame, g.camera, g.chunk.DescriptorSet)
}

func defaultBindings() map[input.Action]input.Binding {
	return map[input.Action]input.Binding{
		actionMoveForward: {
			Key:      glfw.KeyW,
			Cooldown: moveCooldown,
		},
		actionMoveBackward: {
			Key:      glfw.KeyS,
			Cooldown: moveCooldown,
		},
		actionMoveLeft: {
			Key:      glfw.KeyA,
			Cooldown: moveCooldown,
		},
		actionMoveRight: {
			Key:      glfw.KeyD,
			Cooldown: moveCooldown,
		},
		actionYawDown: {
			Key:      glfw.KeyLeft,
			Cooldown: moveCooldown,
		},
		actionYawUp: {
			Key:      glfw.KeyRight,
			Cooldown: moveCooldown,
		},
		actionTurnRight: {
			Key:      glfw.KeyDown,
			Cooldown: moveCooldown,
		},
		actionTurnLeft: {
			Key:      glfw.KeyUp,
			Cooldown: moveCooldown,
		},
		actionMoveAway: {
			Key:      glfw.KeyKPAdd,
			Cooldown: moveCooldown,
		},
		actionMoveCloser: {
			Key:      glfw.KeyKPSubtract,
			Cooldown: moveCooldown,
		},
		actionMoveUp: {
			Key:      glfw.KeyQ,
			Cooldown: moveCooldown,
		},
		actionMoveDown: {
			Key:      glfw.KeyE,
			Cooldown: moveCooldown,
		},
	}
}
