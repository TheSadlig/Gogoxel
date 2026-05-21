package engine

import (
	"fmt"
	"math"
	"strings"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"
)

type CursorSample struct {
	NormalizedX   float32
	NormalizedY   float32
	ViewportWidth int
	ViewportHeight int
}

type EditMode int

const (
	EditModeUnknown EditMode = iota
	EditModePlace
	EditModeRemove
)

const (
	continuousStrokeCursorStep = float32(1.0 / 256.0)
	continuousStrokeMoveStep   = float32(0.25)
	continuousStrokeAngleStep  = float32(0.75)
	maxContinuousStrokeSamples = 8
)

type EditResult struct {
	Changed      bool
	Hit          world.RaycastHit
	TargetVoxel  [3]uint32
	MaterialName string
}

type EditMaterial struct {
	Name  string
	Color uint32
}

type cursorEditSample struct {
	camera platform.Camera
	cursor CursorSample
}

type resolvedCursorEditTarget struct {
	hit      world.RaycastHit
	position [3]uint32
}

type continuousEditState struct {
	active bool
	last   cursorEditSample
	surface *world.SVO
}

var defaultEditMaterials = []EditMaterial{
	{Name: "grass", Color: opaqueColor(0x5A, 0x88, 0x47)},
	{Name: "sand", Color: opaqueColor(0xC9, 0xB5, 0x82)},
	{Name: "stone", Color: opaqueColor(0x7B, 0x83, 0x89)},
	{Name: "wood", Color: opaqueColor(0x8B, 0x5A, 0x2B)},
	{Name: "glass", Color: opaqueColor(0xA9, 0xD6, 0xE5)},
	{Name: "brick", Color: opaqueColor(0xB5, 0x4A, 0x3A)},
}

func EditMaterialNames() []string {
	names := make([]string, len(defaultEditMaterials))
	for index, material := range defaultEditMaterials {
		names[index] = material.Name
	}
	return names
}

func (c *Core) SetSelectedEditMaterial(name string) error {
	if c == nil {
		return fmt.Errorf("engine core is not initialized")
	}
	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" {
		return fmt.Errorf("edit material name is required")
	}
	for index, material := range defaultEditMaterials {
		if material.Name != trimmed {
			continue
		}
		c.selectedEditMaterial = index
		return nil
	}
	return fmt.Errorf("unknown edit material %q", name)
}

func (c *Core) SelectedEditMaterialName() string {
	if c == nil || c.selectedEditMaterial < 0 || c.selectedEditMaterial >= len(defaultEditMaterials) {
		return defaultEditMaterials[0].Name
	}
	return defaultEditMaterials[c.selectedEditMaterial].Name
}

func (c *Core) SetCursorSample(sample CursorSample) {
	if c == nil {
		return
	}
	c.cursor = sample
}

func (c *Core) EditAtCursor(mode EditMode, cursor CursorSample) (EditResult, error) {
	return c.applyCursorEditSamples(mode, []cursorEditSample{{camera: c.camera, cursor: cursor}}, c.svo, true)
}

func (c *Core) applyCursorEditSamples(mode EditMode, samples []cursorEditSample, raycastSVO *world.SVO, strict bool) (EditResult, error) {
	if c == nil {
		return EditResult{}, fmt.Errorf("engine core is not initialized")
	}
	if c.svo == nil {
		return EditResult{}, fmt.Errorf("no scene is loaded")
	}
	if raycastSVO == nil {
		raycastSVO = c.svo
	}
	if len(samples) == 0 {
		return EditResult{MaterialName: c.SelectedEditMaterialName()}, nil
	}

	result := EditResult{MaterialName: c.SelectedEditMaterialName()}
	edits := make([]world.VoxelEdit, 0, len(samples))
	seen := make(map[[3]uint32]struct{}, len(samples))
	color := uint32(0)
	if mode == EditModePlace {
		color = defaultEditMaterials[c.selectedEditMaterial].Color
	}

	for _, sample := range samples {
		target, err := c.resolveCursorEditTarget(mode, sample, raycastSVO)
		if err != nil {
			if strict {
				return EditResult{}, err
			}
			continue
		}

		result.Hit = target.hit
		result.TargetVoxel = target.position

		if _, ok := seen[result.TargetVoxel]; ok {
			continue
		}
		seen[result.TargetVoxel] = struct{}{}
		edits = append(edits, world.VoxelEdit{Position: result.TargetVoxel, Color: color})
	}

	if c.svo.ApplyVoxelEdits(edits) > 0 {
		result.Changed = true
		c.sceneVersion++
	}
	return result, nil
}

func (c *Core) resolveCursorEditTarget(mode EditMode, sample cursorEditSample, raycastSVO *world.SVO) (resolvedCursorEditTarget, error) {
	if c == nil {
		return resolvedCursorEditTarget{}, fmt.Errorf("engine core is not initialized")
	}
	if c.svo == nil {
		return resolvedCursorEditTarget{}, fmt.Errorf("no scene is loaded")
	}
	if raycastSVO == nil {
		raycastSVO = c.svo
	}

	ray, err := cursorRay(sample.camera, sample.cursor)
	if err != nil {
		return resolvedCursorEditTarget{}, err
	}
	hit, ok := raycastSVO.Raycast(ray, float32(raycastSVO.Size())*2)
	if !ok {
		return resolvedCursorEditTarget{}, fmt.Errorf("cursor ray did not hit the scene")
	}

	target := hit.Voxel
	switch mode {
	case EditModePlace:
		target, ok = offsetVoxel(hit.Voxel, hit.Normal, c.svo.Size())
		if !ok {
			return resolvedCursorEditTarget{}, fmt.Errorf("cursor placement target is outside the scene")
		}
	case EditModeRemove:
		// Removing edits the hit voxel directly.
	default:
		return resolvedCursorEditTarget{}, fmt.Errorf("unsupported edit mode %d", mode)
	}

	return resolvedCursorEditTarget{hit: hit, position: target}, nil
}

func (c *Core) continuePlaceStroke() {
	if c == nil {
		return
	}
	current := cursorEditSample{camera: c.camera, cursor: c.cursor}
	if !c.placeStroke.active {
		c.placeStroke = continuousEditState{active: true, last: current}
		return
	}

	samples := interpolateStrokeSamples(c.placeStroke.last, current)
	c.placeStroke.last = current
	if len(samples) == 0 {
		return
	}
	_, _ = c.applyCursorEditSamples(EditModePlace, samples, c.placeStroke.surface, false)
}

func cloneSVO(source *world.SVO) *world.SVO {
	if source == nil {
		return nil
	}
	clone := world.NewSVO()
	if err := clone.LoadSnapshot(source.Snapshot()); err != nil {
		return nil
	}
	return clone
}

func interpolateStrokeSamples(previous, current cursorEditSample) []cursorEditSample {
	steps := continuousStrokeSteps(previous, current)
	if steps == 0 {
		return nil
	}

	samples := make([]cursorEditSample, 0, steps)
	for step := 1; step <= steps; step++ {
		t := float32(step) / float32(steps)
		samples = append(samples, cursorEditSample{
			camera: lerpCamera(previous.camera, current.camera, t),
			cursor: lerpCursorSample(previous.cursor, current.cursor, t),
		})
	}
	return samples
}

func continuousStrokeSteps(previous, current cursorEditSample) int {
	cursorDelta := maxFloat32(
		absFloat32(current.cursor.NormalizedX-previous.cursor.NormalizedX),
		absFloat32(current.cursor.NormalizedY-previous.cursor.NormalizedY),
	)
	moveDelta := distance3(previous.camera.Position, current.camera.Position)
	angleDelta := maxFloat32(
		angularDeltaDegrees(previous.camera.YawDeg, current.camera.YawDeg),
		angularDeltaDegrees(previous.camera.PitchDeg, current.camera.PitchDeg),
	)

	steps := 0
	if cursorDelta > 0 {
		steps = int(math.Ceil(float64(cursorDelta / continuousStrokeCursorStep)))
	}
	if moveSteps := int(math.Ceil(float64(moveDelta / continuousStrokeMoveStep))); moveSteps > steps {
		steps = moveSteps
	}
	if angleSteps := int(math.Ceil(float64(angleDelta / continuousStrokeAngleStep))); angleSteps > steps {
		steps = angleSteps
	}
	if steps > maxContinuousStrokeSamples {
		steps = maxContinuousStrokeSamples
	}
	return steps
}

func lerpCursorSample(a, b CursorSample, t float32) CursorSample {
	return CursorSample{
		NormalizedX:    a.NormalizedX + (b.NormalizedX-a.NormalizedX)*t,
		NormalizedY:    a.NormalizedY + (b.NormalizedY-a.NormalizedY)*t,
		ViewportWidth:  b.ViewportWidth,
		ViewportHeight: b.ViewportHeight,
	}
}

func lerpCamera(a, b platform.Camera, t float32) platform.Camera {
	return platform.Camera{
		Position: [3]float32{
			a.Position[0] + (b.Position[0]-a.Position[0])*t,
			a.Position[1] + (b.Position[1]-a.Position[1])*t,
			a.Position[2] + (b.Position[2]-a.Position[2])*t,
		},
		YawDeg:   a.YawDeg + shortestAngleDelta(a.YawDeg, b.YawDeg)*t,
		PitchDeg: a.PitchDeg + (b.PitchDeg-a.PitchDeg)*t,
		FovDeg:   a.FovDeg + (b.FovDeg-a.FovDeg)*t,
	}
}

func distance3(a, b [3]float32) float32 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func shortestAngleDelta(from, to float32) float32 {
	delta := float32(math.Mod(float64(to-from), 360))
	if delta > 180 {
		delta -= 360
	}
	if delta < -180 {
		delta += 360
	}
	return delta
}

func angularDeltaDegrees(a, b float32) float32 {
	return absFloat32(shortestAngleDelta(a, b))
}

func (c *Core) cycleSelectedEditMaterial(step int) {
	if c == nil || len(defaultEditMaterials) == 0 || step == 0 {
		return
	}
	next := c.selectedEditMaterial + step
	for next < 0 {
		next += len(defaultEditMaterials)
	}
	c.selectedEditMaterial = next % len(defaultEditMaterials)
}

func cursorRay(camera platform.Camera, cursor CursorSample) (world.Ray, error) {
	if cursor.ViewportWidth <= 0 || cursor.ViewportHeight <= 0 {
		return world.Ray{}, fmt.Errorf("cursor viewport dimensions must be positive")
	}
	normalizedX := clampFloat(cursor.NormalizedX, 0, 1)
	normalizedY := clampFloat(cursor.NormalizedY, 0, 1)
	screenX := normalizedX*2 - 1
	screenY := normalizedY*2 - 1
	aspect := float32(cursor.ViewportWidth) / float32(cursor.ViewportHeight)
	fovScale := float32(math.Tan(float64(camera.FovDeg) * 0.5 * math.Pi / 180.0))
	screenX *= aspect * fovScale
	screenY *= fovScale

	forward := camera.Forward()
	right := camera.Right()
	up := camera.Up()
	direction := normalize3([3]float32{
		forward[0] + right[0]*screenX + up[0]*screenY,
		forward[1] + right[1]*screenX + up[1]*screenY,
		forward[2] + right[2]*screenX + up[2]*screenY,
	})
	return world.Ray{Origin: camera.Position, Direction: direction}, nil
}

func offsetVoxel(voxel [3]uint32, normal [3]int32, worldSize uint) ([3]uint32, bool) {
	target := [3]int64{int64(voxel[0]), int64(voxel[1]), int64(voxel[2])}
	for axis := 0; axis < 3; axis++ {
		target[axis] += int64(normal[axis])
		if target[axis] < 0 || target[axis] >= int64(worldSize) {
			return [3]uint32{}, false
		}
	}
	return [3]uint32{uint32(target[0]), uint32(target[1]), uint32(target[2])}, true
}

func normalize3(vector [3]float32) [3]float32 {
	length := float32(math.Sqrt(float64(vector[0]*vector[0] + vector[1]*vector[1] + vector[2]*vector[2])))
	if length == 0 {
		return [3]float32{}
	}
	return [3]float32{vector[0] / length, vector[1] / length, vector[2] / length}
}

func clampFloat(value, minValue, maxValue float32) float32 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func opaqueColor(red, green, blue byte) uint32 {
	return uint32(red) | uint32(green)<<8 | uint32(blue)<<16 | 0xFF000000
}
