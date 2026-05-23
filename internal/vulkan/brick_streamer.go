package vulkan

import (
	"container/heap"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"Gogoxel/internal/platform"
	"Gogoxel/internal/world"

	vk "github.com/vulkan-go/vulkan"
)

const (
	defaultBrickUploadBudget = 192
	maxBrickUploadBudget     = 384
	uploadBudgetMotionGain   = 48
	svoHeaderWordCount       = 2
	// halfBrickWorldUnits is the camera-movement threshold that triggers a
	// streaming replan: half a brick edge ensures we never miss a brick
	// crossing between replans, while avoiding redundant planner work for
	// sub-brick motion.
	halfBrickWorldUnits    float32 = float32(brickSizeVoxels) * 0.5
	nearFieldWorldUnits    float32 = float32(brickSizeVoxels) * 3
	motionFieldWorldUnits  float32 = float32(brickSizeVoxels) * 4
	motionPrefetchFrames   float32 = 4
	motionPrefetchMaxUnits float32 = float32(brickSizeVoxels) * 16
	viewConeMargin                 = 1.35
	turnReplanDegrees      float64 = 8
)

type streamBrick struct {
	nodeIndex uint32
	origin    [3]uint32
	center    [3]float32
	// voxels references the SVO's backing voxel array directly to avoid a
	// 512B copy per brick at streamer construction. The streamer keeps a
	// reference to the source slice (`sourceBricks`) to keep the backing
	// data alive for its lifetime.
	voxels *[world.BrickVoxelCount]uint8
}

type residentBrick struct {
	slot uint32
	lru  uint64
}

type brickUploadOp struct {
	logicalIndex      int
	slot              uint32
	evictedLogicalIdx int
}

type brickEvictOp struct {
	logicalIndex int
	slot         uint32
}

type scenePointerPatch struct {
	nodeIndex uint32
	slot      uint32
}

type sceneResidentUpload struct {
	logicalIndex int
	slot         uint32
}

type sceneUpdatePlan struct {
	pointerPatches []scenePointerPatch
	uploads        []sceneResidentUpload
}

// streamPlan is the immutable result of planning. Recording consumes it and
// only commits the state changes (residency-map mutation, pool frees) after
// the plan has been successfully transformed into GPU commands.
type streamPlan struct {
	uploads        []brickUploadOp
	evictions      []brickEvictOp
	residencyDelta []residencyDelta
	allocations    []uint32 // fresh slots; freed on rollback only
	frees          []uint32 // victim slots freed on commit only
}

type residencyDelta struct {
	logicalIndex int
	add          bool
	slot         uint32
	lru          uint64
}

type brickStreamer struct {
	mu sync.Mutex

	// sourceBricks owns a snapshot of brick metadata for the last committed
	// scene state. The slice is copied on replacement so SVO slice-backing
	// reuse cannot mutate the streamer's previous-scene view underfoot.
	sourceBricks []world.Brick
	bricks       []streamBrick
	sceneOrigin  [3]int32

	camera          platform.Camera
	cameraMotion    [3]float32
	cameraEverSet   bool
	lastPlannedPos  [3]float32
	lastPlannedFwd  [3]float32
	lastPlannedOnce bool
	desired         []int
	desiredReady    bool
	resident        map[int]residentBrick

	residentCap     int
	residentLimit   int
	uploadBudget    int
	maxUploadBudget int
	lruTick         uint64
	sceneRevision   uint64

	// Reused planner scratch.
	desiredHeap brickPriorityHeap

	stopCh   chan struct{}
	doneCh   chan struct{}
	dirty    chan struct{}
	stopOnce sync.Once
}

type StreamingStats struct {
	DesiredReady        bool
	DesiredCount        int
	ResidentCount       int
	PendingDesiredCount int
	ResidentLimit       int
	UploadBudget        int
}

// brickStreamerConfig customises the streaming budget. Zero-valued fields
// fall back to defaults derived from brick-pool capacity.
type brickStreamerConfig struct {
	ResidentLimit int
	UploadBudget  int
}

func newBrickStreamer(bricks []world.Brick) *brickStreamer {
	return newBrickStreamerWithConfig(bricks, brickStreamerConfig{})
}

func newBrickStreamerWithConfig(bricks []world.Brick, cfg brickStreamerConfig) *brickStreamer {
	if len(bricks) == 0 {
		return nil
	}
	metadata := copyBrickMetadata(nil, bricks)

	residentCap := cfg.ResidentLimit
	if residentCap <= 0 {
		// Use the full brick-pool capacity (minus slot 0 reserved for air).
		residentCap = int(brickPoolCapacity) - 1
	}
	if residentCap <= 0 {
		residentCap = 1
	}
	residentLimit := clampResidentLimit(residentCap, len(bricks))

	uploadBudget := cfg.UploadBudget
	if uploadBudget <= 0 {
		uploadBudget = defaultBrickUploadBudget
	}
	maxUploadBudget := uploadBudget
	if maxUploadBudget < maxBrickUploadBudget {
		maxUploadBudget = maxBrickUploadBudget
	}

	streamer := &brickStreamer{
		sourceBricks:    metadata,
		bricks:          rebuildStreamBricks(nil, metadata),
		resident:        make(map[int]residentBrick, residentLimit),
		residentCap:     residentCap,
		residentLimit:   residentLimit,
		uploadBudget:    uploadBudget,
		maxUploadBudget: maxUploadBudget,
		sceneRevision:   1,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
		// dirty is 1-buffered so SetCameraPosition can signal without blocking
		// and signals coalesce until the planner consumes one.
		dirty: make(chan struct{}, 1),
	}
	go streamer.run()
	return streamer
}

func copyBrickMetadata(dst, bricks []world.Brick) []world.Brick {
	if len(bricks) == 0 {
		return nil
	}
	if cap(dst) < len(bricks) {
		dst = make([]world.Brick, len(bricks))
	} else {
		dst = dst[:len(bricks)]
	}
	copy(dst, bricks)
	return dst
}

func rebuildStreamBricks(dst []streamBrick, metadata []world.Brick) []streamBrick {
	if len(metadata) == 0 {
		return nil
	}
	if cap(dst) < len(metadata) {
		dst = make([]streamBrick, len(metadata))
	} else {
		dst = dst[:len(metadata)]
	}
	for index := range metadata {
		brick := &metadata[index]
		dst[index] = streamBrick{
			nodeIndex: brick.NodeIndex,
			origin:    brick.Origin,
			center: [3]float32{
				float32(brick.Origin[0]) + float32(brickSizeVoxels)*0.5,
				float32(brick.Origin[1]) + float32(brickSizeVoxels)*0.5,
				float32(brick.Origin[2]) + float32(brickSizeVoxels)*0.5,
			},
			voxels: brick.Voxels,
		}
	}
	return dst
}

func clampResidentLimit(residentCap int, brickCount int) int {
	if residentCap <= 0 || brickCount <= 0 {
		return 0
	}
	if residentCap > brickCount {
		return brickCount
	}
	return residentCap
}

func (s *brickStreamer) run() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			return
		case <-s.dirty:
			s.recomputeDesired()
		}
	}
}

func (s *brickStreamer) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
}

// markDirty signals the planner without blocking. Extra signals coalesce.
func (s *brickStreamer) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *brickStreamer) SetCamera(camera platform.Camera) {
	if s == nil {
		return
	}

	s.mu.Lock()
	previousCamera := s.camera
	hadPreviousCamera := s.cameraEverSet
	s.camera = camera
	if hadPreviousCamera {
		s.cameraMotion = [3]float32{
			camera.Position[0] - previousCamera.Position[0],
			camera.Position[1] - previousCamera.Position[1],
			camera.Position[2] - previousCamera.Position[2],
		}
	} else {
		s.cameraMotion = [3]float32{}
	}
	firstSet := !hadPreviousCamera
	s.cameraEverSet = true

	movedFarEnough := !s.lastPlannedOnce
	turnedFarEnough := !s.lastPlannedOnce
	if s.lastPlannedOnce {
		dx := camera.Position[0] - s.lastPlannedPos[0]
		dy := camera.Position[1] - s.lastPlannedPos[1]
		dz := camera.Position[2] - s.lastPlannedPos[2]
		movedFarEnough = dx*dx+dy*dy+dz*dz >= halfBrickWorldUnits*halfBrickWorldUnits
		turnedFarEnough = cameraTurnedEnough(s.lastPlannedFwd, camera.Forward())
	}
	s.mu.Unlock()

	if firstSet || movedFarEnough || turnedFarEnough {
		s.markDirty()
	}
}

func (s *brickStreamer) primeCamera(camera platform.Camera) {
	if s == nil {
		return
	}
	s.mu.Lock()
	bricks := append([]streamBrick(nil), s.bricks...)
	residentLimit := s.residentLimit
	s.mu.Unlock()
	desired := computeDesiredSnapshot(bricks, residentLimit, camera, [3]float32{})
	forward := camera.Forward()
	s.mu.Lock()
	s.camera = camera
	s.cameraMotion = [3]float32{}
	s.cameraEverSet = true
	s.desired = desired
	s.desiredReady = true
	s.lastPlannedPos = camera.Position
	s.lastPlannedFwd = forward
	s.lastPlannedOnce = true
	s.mu.Unlock()
}

func (s *brickStreamer) setSceneOrigin(sceneOrigin [3]int32) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sceneOrigin = sceneOrigin
	s.mu.Unlock()
}

func (s *brickStreamer) ResidentCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.resident)
}

func (s *brickStreamer) Stats() StreamingStats {
	if s == nil {
		return StreamingStats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureDesiredIndicesLocked()

	stats := StreamingStats{
		DesiredReady:  s.desiredReady,
		DesiredCount:  len(s.desired),
		ResidentCount: len(s.resident),
		ResidentLimit: s.residentLimit,
		UploadBudget:  s.currentUploadBudgetLocked(),
	}
	if !s.desiredReady {
		return stats
	}
	for _, logicalIndex := range s.desired {
		if _, ok := s.resident[logicalIndex]; ok {
			continue
		}
		stats.PendingDesiredCount++
	}
	return stats
}

func (s *brickStreamer) pendingDesiredCountLocked() int {
	if s == nil || !s.desiredReady {
		return 0
	}
	s.ensureDesiredIndicesLocked()
	pending := 0
	for _, logicalIndex := range s.desired {
		if _, ok := s.resident[logicalIndex]; ok {
			continue
		}
		pending++
	}
	return pending
}

func (s *brickStreamer) currentUploadBudgetLocked() int {
	if s == nil {
		return 0
	}
	budget := s.uploadBudget
	motionSq := s.cameraMotion[0]*s.cameraMotion[0] + s.cameraMotion[1]*s.cameraMotion[1] + s.cameraMotion[2]*s.cameraMotion[2]
	if motionSq >= halfBrickWorldUnits*halfBrickWorldUnits {
		motionSteps := int(math.Ceil(math.Sqrt(float64(motionSq)) / float64(halfBrickWorldUnits)))
		budget += motionSteps * uploadBudgetMotionGain
	}
	if pending := s.pendingDesiredCountLocked(); pending > budget {
		budget = pending
	}
	if budget < s.uploadBudget {
		budget = s.uploadBudget
	}
	if budget > s.maxUploadBudget {
		budget = s.maxUploadBudget
	}
	return budget
}

func (s *brickStreamer) recomputeDesired() {
	if s == nil {
		return
	}

	s.mu.Lock()
	camera := s.camera
	cameraMotion := s.cameraMotion
	bricks := append([]streamBrick(nil), s.bricks...)
	residentLimit := s.residentLimit
	sceneRevision := s.sceneRevision
	s.mu.Unlock()

	desired := computeDesiredSnapshot(bricks, residentLimit, camera, cameraMotion)
	forward := camera.Forward()

	s.mu.Lock()
	if s.sceneRevision != sceneRevision {
		s.mu.Unlock()
		return
	}
	s.desired = desired
	s.desiredReady = true
	s.lastPlannedPos = camera.Position
	s.lastPlannedFwd = forward
	s.lastPlannedOnce = true
	s.mu.Unlock()
}

// brickPriority is the planner key for a resident brick candidate.
// Lower priorityBand is better: keep the near field first, then the current
// view cone, then the front hemisphere, then everything behind the camera.
type brickPriority struct {
	logicalIndex int
	priorityBand uint8
	distSq       float32
	alignment    float32
}

// brickPriorityHeap is a max-heap over the planner priority, used to maintain
// the best K bricks in O(N log K) instead of sorting the whole scene.
type brickPriorityHeap []brickPriority

func (h brickPriorityHeap) Len() int { return len(h) }
func (h brickPriorityHeap) Less(i, j int) bool {
	if h[i].priorityBand != h[j].priorityBand {
		return h[i].priorityBand > h[j].priorityBand
	}
	if h[i].distSq != h[j].distSq {
		return h[i].distSq > h[j].distSq
	}
	if h[i].alignment != h[j].alignment {
		return h[i].alignment < h[j].alignment
	}
	return h[i].logicalIndex > h[j].logicalIndex
}
func (h brickPriorityHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *brickPriorityHeap) Push(x any)   { *h = append(*h, x.(brickPriority)) }
func (h *brickPriorityHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func (s *brickStreamer) computeDesired(camera platform.Camera, cameraMotion [3]float32) []int {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	bricks := append([]streamBrick(nil), s.bricks...)
	residentLimit := s.residentLimit
	s.mu.Unlock()
	return computeDesiredSnapshot(bricks, residentLimit, camera, cameraMotion)
}

func (s *brickStreamer) computeDesiredLocked(camera platform.Camera, cameraMotion [3]float32) []int {
	if s == nil {
		return nil
	}
	return computeDesiredSnapshot(s.bricks, s.residentLimit, camera, cameraMotion)
}

func computeDesiredSnapshot(bricks []streamBrick, residentLimit int, camera platform.Camera, cameraMotion [3]float32) []int {
	limit := min(residentLimit, len(bricks))
	if limit <= 0 {
		return nil
	}
	prefetchPosition, hasMotionPrefetch := predictiveCameraPosition(camera.Position, cameraMotion)

	var desiredHeap brickPriorityHeap
	heapSlice := desiredHeap[:0]

	for index, brick := range bricks {
		candidate := prioritizeBrick(camera, prefetchPosition, hasMotionPrefetch, brick.center, index)
		if len(heapSlice) < limit {
			heapSlice = append(heapSlice, candidate)
			if len(heapSlice) == limit {
				desiredHeap = heapSlice
				heap.Init(&desiredHeap)
				heapSlice = desiredHeap
			}
			continue
		}
		// Heap is full: replace root if this brick outranks the current worst.
		if betterBrickPriority(candidate, heapSlice[0]) {
			heapSlice[0] = candidate
			desiredHeap = heapSlice
			heap.Fix(&desiredHeap, 0)
			heapSlice = desiredHeap
		}
	}

	// Drain the max-heap to get farthest-first order, then reverse for
	// closest-first deterministic planning.
	out := make([]int, len(heapSlice))
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&desiredHeap).(brickPriority).logicalIndex
	}
	return out
}

func (s *brickStreamer) ensureDesiredIndicesLocked() {
	if s == nil || !s.desiredReady {
		return
	}
	if desiredIndicesValid(s.desired, len(s.bricks)) {
		return
	}
	s.desired = s.computeDesiredLocked(s.camera, s.cameraMotion)
}

func desiredIndicesValid(desired []int, brickCount int) bool {
	for _, logicalIndex := range desired {
		if logicalIndex < 0 || logicalIndex >= brickCount {
			return false
		}
	}
	return true
}

func betterBrickPriority(a, b brickPriority) bool {
	if a.priorityBand != b.priorityBand {
		return a.priorityBand < b.priorityBand
	}
	if a.distSq != b.distSq {
		return a.distSq < b.distSq
	}
	if a.alignment != b.alignment {
		return a.alignment > b.alignment
	}
	return a.logicalIndex < b.logicalIndex
}

func prioritizeBrick(camera platform.Camera, prefetchPosition [3]float32, hasMotionPrefetch bool, center [3]float32, logicalIndex int) brickPriority {
	currentDistSq := squaredDistance(camera.Position, center)
	if currentDistSq <= nearFieldWorldUnits*nearFieldWorldUnits {
		return brickPriority{logicalIndex: logicalIndex, priorityBand: 0, distSq: currentDistSq, alignment: 1}
	}
	if hasMotionPrefetch {
		prefetchDistSq := squaredDistance(prefetchPosition, center)
		if prefetchDistSq <= motionFieldWorldUnits*motionFieldWorldUnits {
			return brickPriority{logicalIndex: logicalIndex, priorityBand: 1, distSq: prefetchDistSq, alignment: 1}
		}
	}

	forward := camera.Forward()
	offset := [3]float32{center[0] - camera.Position[0], center[1] - camera.Position[1], center[2] - camera.Position[2]}
	distance := float32(math.Sqrt(float64(currentDistSq)))
	alignment := float32(1)
	if distance > 0 {
		invDistance := 1 / distance
		alignment = (offset[0]*forward[0] + offset[1]*forward[1] + offset[2]*forward[2]) * invDistance
	}

	priorityBand := uint8(4)
	if alignment >= expandedViewCos(camera) {
		priorityBand = 2
	} else if alignment >= 0 {
		priorityBand = 3
	}
	return brickPriority{logicalIndex: logicalIndex, priorityBand: priorityBand, distSq: currentDistSq, alignment: alignment}
}

func predictiveCameraPosition(position, motion [3]float32) ([3]float32, bool) {
	motionSq := motion[0]*motion[0] + motion[1]*motion[1] + motion[2]*motion[2]
	if motionSq < halfBrickWorldUnits*halfBrickWorldUnits {
		return position, false
	}
	motionLength := float32(math.Sqrt(float64(motionSq)))
	if motionLength == 0 {
		return position, false
	}
	lookAhead := motionLength * motionPrefetchFrames
	if lookAhead > motionPrefetchMaxUnits {
		lookAhead = motionPrefetchMaxUnits
	}
	scale := lookAhead / motionLength
	return [3]float32{
		position[0] + motion[0]*scale,
		position[1] + motion[1]*scale,
		position[2] + motion[2]*scale,
	}, true
}

func expandedViewCos(camera platform.Camera) float32 {
	halfFovRad := float64(camera.FovDeg) * math.Pi / 360
	if halfFovRad <= 0 {
		halfFovRad = math.Pi / 6
	}
	expandedHalfFov := halfFovRad * viewConeMargin
	maxHalfFov := math.Pi * 0.95
	if expandedHalfFov > maxHalfFov {
		expandedHalfFov = maxHalfFov
	}
	return float32(math.Cos(expandedHalfFov))
}

func cameraTurnedEnough(previousForward, nextForward [3]float32) bool {
	dot := previousForward[0]*nextForward[0] + previousForward[1]*nextForward[1] + previousForward[2]*nextForward[2]
	return dot <= float32(math.Cos(turnReplanDegrees*math.Pi/180))
}

func squaredDistance(a, b [3]float32) float32 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return dx*dx + dy*dy + dz*dz
}

// planOps produces an immutable plan and pre-reserves brick-pool slots. The
// reservations are tracked in plan.allocations so commitPlan / rollbackPlan
// can keep pool state consistent with what was actually recorded.
func (s *brickStreamer) planOps(pool *brickPool) streamPlan {
	if s == nil || pool == nil {
		return streamPlan{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.bricks) == 0 || !s.desiredReady {
		return streamPlan{}
	}
	s.ensureDesiredIndicesLocked()

	plan := streamPlan{}
	currentUploadBudget := s.currentUploadBudgetLocked()

	desiredSet := make(map[int]struct{}, len(s.desired))
	for _, logicalIndex := range s.desired {
		if logicalIndex < 0 || logicalIndex >= len(s.bricks) {
			continue
		}
		desiredSet[logicalIndex] = struct{}{}
	}

	// Touch LRU for already-resident desired bricks. Slightly-stale LRU on
	// recording failure is harmless.
	for _, logicalIndex := range s.desired {
		if logicalIndex < 0 || logicalIndex >= len(s.bricks) {
			continue
		}
		if resident, ok := s.resident[logicalIndex]; ok {
			s.lruTick++
			resident.lru = s.lruTick
			s.resident[logicalIndex] = resident
		}
	}

	plan.uploads = make([]brickUploadOp, 0, min(len(s.desired), currentUploadBudget))
	plan.residencyDelta = make([]residencyDelta, 0, currentUploadBudget*2)

	for _, logicalIndex := range s.desired {
		if len(plan.uploads) >= currentUploadBudget {
			break
		}
		if logicalIndex < 0 || logicalIndex >= len(s.bricks) {
			continue
		}
		if _, ok := s.resident[logicalIndex]; ok {
			continue
		}

		slot, evictedLogicalIdx, allocatedFresh, ok := s.reserveSlot(pool, desiredSet)
		if !ok {
			break
		}
		if allocatedFresh {
			plan.allocations = append(plan.allocations, slot)
		}
		if evictedLogicalIdx >= 0 {
			plan.residencyDelta = append(plan.residencyDelta, residencyDelta{
				logicalIndex: evictedLogicalIdx,
				add:          false,
			})
		}

		s.lruTick++
		plan.residencyDelta = append(plan.residencyDelta, residencyDelta{
			logicalIndex: logicalIndex,
			add:          true,
			slot:         slot,
			lru:          s.lruTick,
		})
		plan.uploads = append(plan.uploads, brickUploadOp{
			logicalIndex:      logicalIndex,
			slot:              slot,
			evictedLogicalIdx: evictedLogicalIdx,
		})
	}

	return plan
}

// commitPlan applies a successfully-recorded plan to the streamer's residency
// map and frees evicted pool slots.
func (s *brickStreamer) commitPlan(pool *brickPool, plan streamPlan) {
	if s == nil {
		return
	}
	s.mu.Lock()
	for _, delta := range plan.residencyDelta {
		if delta.add {
			s.resident[delta.logicalIndex] = residentBrick{slot: delta.slot, lru: delta.lru}
		} else {
			delete(s.resident, delta.logicalIndex)
		}
	}
	s.mu.Unlock()
	for _, slot := range plan.frees {
		pool.Free(slot)
	}
}

// rollbackPlan undoes the fresh pool-slot allocations done by planOps after a
// recording failure. The residency map is left untouched.
func (s *brickStreamer) rollbackPlan(pool *brickPool, plan streamPlan) {
	for _, slot := range plan.allocations {
		pool.Free(slot)
	}
}

// reserveSlot returns (slot, evictedLogicalIndex, allocatedFresh, ok).
//   - allocatedFresh = true when the slot came from pool.Allocate (must be
//     freed on rollback). When evicting we reuse the victim's slot without
//     touching the pool.
func (s *brickStreamer) reserveSlot(pool *brickPool, desiredSet map[int]struct{}) (uint32, int, bool, bool) {
	if len(s.resident) < s.residentLimit {
		slot, err := pool.Allocate()
		if err == nil {
			return slot, -1, true, true
		}
	}

	victim := s.pickVictim(desiredSet)
	if victim < 0 {
		slot, err := pool.Allocate()
		if err != nil {
			return 0, -1, false, false
		}
		return slot, -1, true, true
	}

	return s.resident[victim].slot, victim, false, true
}

func (s *brickStreamer) pickVictim(desiredSet map[int]struct{}) int {
	victim := -1
	victimLRU := uint64(0)
	for logicalIndex, resident := range s.resident {
		if _, ok := desiredSet[logicalIndex]; ok {
			continue
		}
		if victim == -1 || resident.lru < victimLRU {
			victim = logicalIndex
			victimLRU = resident.lru
		}
	}
	return victim
}

func (s *brickStreamer) replaceSceneBricks(pool *brickPool, sceneOrigin [3]int32, bricks []world.Brick) sceneUpdatePlan {
	if s == nil {
		return sceneUpdatePlan{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	type residentOrigin struct {
		logicalIndex int
		resident     residentBrick
		voxels       *[world.BrickVoxelCount]uint8
	}

	residentByOrigin := make(map[[3]int32]residentOrigin, len(s.resident))
	for logicalIndex, resident := range s.resident {
		if logicalIndex < 0 || logicalIndex >= len(s.sourceBricks) {
			continue
		}
		brick := s.sourceBricks[logicalIndex]
		residentByOrigin[stableBrickOrigin(s.sceneOrigin, brick.Origin)] = residentOrigin{
			logicalIndex: logicalIndex,
			resident:     resident,
			voxels:       brick.Voxels,
		}
	}
	s.sceneOrigin = sceneOrigin

	metadata := copyBrickMetadata(s.sourceBricks, bricks)
	s.sourceBricks = metadata
	s.bricks = rebuildStreamBricks(s.bricks, metadata)
	s.residentLimit = clampResidentLimit(s.residentCap, len(bricks))
	s.sceneRevision++

	plan := sceneUpdatePlan{
		pointerPatches: make([]scenePointerPatch, 0, len(residentByOrigin)),
		uploads:        make([]sceneResidentUpload, 0, len(residentByOrigin)),
	}
	newResident := make(map[int]residentBrick, min(len(s.resident), len(bricks)))
	for logicalIndex, brick := range bricks {
		existing, ok := residentByOrigin[stableBrickOrigin(sceneOrigin, brick.Origin)]
		if !ok {
			continue
		}
		newResident[logicalIndex] = existing.resident
		plan.pointerPatches = append(plan.pointerPatches, scenePointerPatch{nodeIndex: brick.NodeIndex, slot: existing.resident.slot})
		if !equalBrickVoxelPointers(existing.voxels, brick.Voxels) {
			plan.uploads = append(plan.uploads, sceneResidentUpload{logicalIndex: logicalIndex, slot: existing.resident.slot})
		}
		delete(residentByOrigin, stableBrickOrigin(sceneOrigin, brick.Origin))
	}
	for _, removed := range residentByOrigin {
		if pool != nil {
			pool.Free(removed.resident.slot)
		}
	}
	s.resident = newResident

	if len(s.bricks) == 0 {
		s.desired = nil
		s.desiredReady = true
		return plan
	}
	if s.cameraEverSet {
		s.desired = s.computeDesiredLocked(s.camera, s.cameraMotion)
		s.desiredReady = true
		s.lastPlannedPos = s.camera.Position
		s.lastPlannedFwd = s.camera.Forward()
		s.lastPlannedOnce = true
		return plan
	}
	s.desired = nil
	s.desiredReady = false
	s.lastPlannedOnce = false
	return plan
}

func stableBrickOrigin(sceneOrigin [3]int32, brickOrigin [3]uint32) [3]int32 {
	return [3]int32{
		int32(brickOrigin[0]) + sceneOrigin[0],
		int32(brickOrigin[1]) + sceneOrigin[1],
		int32(brickOrigin[2]) + sceneOrigin[2],
	}
}

func equalBrickVoxelPointers(left, right *[world.BrickVoxelCount]uint8) bool {
	if left == right {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func (chunk *ChunkResources) SetCamera(camera platform.Camera) {
	if chunk == nil {
		return
	}
	firstSet := !chunk.cameraEverSet
	chunk.camera = camera
	chunk.cameraEverSet = true
	if chunk.streamer == nil {
		return
	}
	// On the very first camera position, prime the streamer synchronously so
	// that RecordStreaming in the same frame (e.g. immediately after InitChunk)
	// finds desiredReady=true and can upload bricks without waiting for the
	// background planner goroutine to wake up.
	if firstSet {
		chunk.streamer.primeCamera(camera)
		return
	}
	chunk.streamer.SetCamera(camera)
}

func (chunk *ChunkResources) ResidentBrickCount() int {
	if chunk == nil || chunk.streamer == nil {
		return 0
	}
	return chunk.streamer.ResidentCount()
}

func (chunk *ChunkResources) StreamingStats() StreamingStats {
	if chunk == nil || chunk.streamer == nil {
		return StreamingStats{}
	}
	return chunk.streamer.Stats()
}

func (chunk *ChunkResources) RecordStreaming(frame *Frame) error {
	if chunk == nil || frame == nil || chunk.streamer == nil || chunk.brickPool == nil {
		return nil
	}

	plan := chunk.streamer.planOps(chunk.brickPool)
	if len(plan.uploads) == 0 && len(plan.evictions) == 0 {
		return nil
	}

	if err := chunk.recordPlan(frame, plan); err != nil {
		chunk.streamer.rollbackPlan(chunk.brickPool, plan)
		return err
	}

	chunk.streamer.commitPlan(chunk.brickPool, plan)
	return nil
}

func (chunk *ChunkResources) recordPlan(frame *Frame, plan streamPlan) error {
	if len(plan.uploads) > 0 {
		frame.renderer.transitionImageLayout(
			frame.CommandBuffer,
			chunk.brickPool.image,
			vk.ImageLayoutShaderReadOnlyOptimal,
			vk.ImageLayoutTransferDstOptimal,
			vk.AccessFlags(vk.AccessShaderReadBit),
			vk.AccessFlags(vk.AccessTransferWriteBit),
			vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
			vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		)
	}

	bufferTouched := false
	for _, upload := range plan.uploads {
		if upload.logicalIndex < 0 || upload.logicalIndex >= len(chunk.streamer.bricks) {
			return fmt.Errorf("streaming upload logical index %d out of range for %d bricks", upload.logicalIndex, len(chunk.streamer.bricks))
		}
		if upload.evictedLogicalIdx >= 0 {
			if upload.evictedLogicalIdx >= len(chunk.streamer.bricks) {
				return fmt.Errorf("streaming eviction logical index %d out of range for %d bricks", upload.evictedLogicalIdx, len(chunk.streamer.bricks))
			}
			chunk.patchNodePointer(frame, chunk.streamer.bricks[upload.evictedLogicalIdx].nodeIndex, 0)
			bufferTouched = true
		}

		if err := chunk.recordBrickUpload(frame, upload.logicalIndex, upload.slot); err != nil {
			return err
		}

		chunk.patchNodePointer(frame, chunk.streamer.bricks[upload.logicalIndex].nodeIndex, upload.slot)
		bufferTouched = true
	}

	for _, eviction := range plan.evictions {
		if eviction.logicalIndex < 0 || eviction.logicalIndex >= len(chunk.streamer.bricks) {
			return fmt.Errorf("streaming plan eviction logical index %d out of range for %d bricks", eviction.logicalIndex, len(chunk.streamer.bricks))
		}
		chunk.patchNodePointer(frame, chunk.streamer.bricks[eviction.logicalIndex].nodeIndex, 0)
		bufferTouched = true
	}

	if len(plan.uploads) > 0 {
		frame.renderer.transitionImageLayout(
			frame.CommandBuffer,
			chunk.brickPool.image,
			vk.ImageLayoutTransferDstOptimal,
			vk.ImageLayoutShaderReadOnlyOptimal,
			vk.AccessFlags(vk.AccessTransferWriteBit),
			vk.AccessFlags(vk.AccessShaderReadBit),
			vk.PipelineStageFlags(vk.PipelineStageTransferBit),
			vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
		)
	}

	if bufferTouched {
		frame.renderer.recordBufferShaderBarrier(frame.CommandBuffer, chunk.buffer, chunk.bufferBytes)
	}

	return nil
}

// patchNodePointer issues a 4-byte vkCmdFillBuffer to update the node's
// childPointer in place — no staging buffer or cleanup required.
func (chunk *ChunkResources) patchNodePointer(frame *Frame, nodeIndex, value uint32) {
	vk.CmdFillBuffer(
		frame.CommandBuffer,
		chunk.buffer,
		nodeChildPointerByteOffset(nodeIndex),
		vk.DeviceSize(4),
		value,
	)
}

func (chunk *ChunkResources) recordBrickUpload(frame *Frame, logicalIndex int, slot uint32) error {
	brickX, brickY, brickZ, err := chunk.brickPool.slotCoord(slot)
	if err != nil {
		return err
	}

	const brickBytes = vk.DeviceSize(world.BrickVoxelCount)
	stagingBuffer, stagingOffset, dst, release, err := frame.renderer.stagingAllocForUpload(frame.FrameSlot, brickBytes, 4)
	if err != nil {
		return fmt.Errorf("staging-alloc for brick upload: %w", err)
	}
	if release != nil {
		frame.renderer.deferFrameRelease(frame.FrameSlot, release)
	}
	src := chunk.streamer.bricks[logicalIndex].voxels[:]
	copy(unsafe.Slice((*byte)(dst), len(src)), src)

	regions := []vk.BufferImageCopy{{
		BufferOffset:      stagingOffset,
		BufferRowLength:   0,
		BufferImageHeight: 0,
		ImageSubresource: vk.ImageSubresourceLayers{
			AspectMask:     vk.ImageAspectFlags(vk.ImageAspectColorBit),
			MipLevel:       0,
			BaseArrayLayer: 0,
			LayerCount:     1,
		},
		ImageOffset: vk.Offset3D{
			X: int32(brickX * brickSizeVoxels),
			Y: int32(brickY * brickSizeVoxels),
			Z: int32(brickZ * brickSizeVoxels),
		},
		ImageExtent: vk.Extent3D{
			Width:  brickSizeVoxels,
			Height: brickSizeVoxels,
			Depth:  brickSizeVoxels,
		},
	}}
	vk.CmdCopyBufferToImage(frame.CommandBuffer, stagingBuffer, chunk.brickPool.image, vk.ImageLayoutTransferDstOptimal, uint32(len(regions)), regions)
	return nil
}

func nodeChildPointerByteOffset(nodeIndex uint32) vk.DeviceSize {
	return vk.DeviceSize((svoHeaderWordCount + int(nodeIndex)*2 + 1) * 4)
}

// recordBufferTransferBarrier inserts a TRANSFER→TRANSFER pipeline barrier on
// buffer. Use this when a vkCmdCopyBuffer and subsequent vkCmdFillBuffer (or
// any two transfer writes) target overlapping regions of the same buffer:
// without it the GPU may reorder the writes, leaving the earlier write's data
// overwritten or invisible to the later write.
func (r *Renderer) recordBufferTransferBarrier(commandBuffer vk.CommandBuffer, buffer vk.Buffer, size vk.DeviceSize) {
	barriers := []vk.BufferMemoryBarrier{{
		SType:               vk.StructureTypeBufferMemoryBarrier,
		SrcAccessMask:       vk.AccessFlags(vk.AccessTransferWriteBit),
		DstAccessMask:       vk.AccessFlags(vk.AccessTransferWriteBit),
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored,
		DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Buffer:              buffer,
		Offset:              0,
		Size:                size,
	}}
	vk.CmdPipelineBarrier(
		commandBuffer,
		vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		0,
		0,
		nil,
		uint32(len(barriers)),
		barriers,
		0,
		nil,
	)
}

func (r *Renderer) recordBufferShaderBarrier(commandBuffer vk.CommandBuffer, buffer vk.Buffer, size vk.DeviceSize) {
	barriers := []vk.BufferMemoryBarrier{{
		SType:               vk.StructureTypeBufferMemoryBarrier,
		SrcAccessMask:       vk.AccessFlags(vk.AccessTransferWriteBit),
		DstAccessMask:       vk.AccessFlags(vk.AccessShaderReadBit),
		SrcQueueFamilyIndex: vk.QueueFamilyIgnored,
		DstQueueFamilyIndex: vk.QueueFamilyIgnored,
		Buffer:              buffer,
		Offset:              0,
		Size:                size,
	}}
	vk.CmdPipelineBarrier(
		commandBuffer,
		vk.PipelineStageFlags(vk.PipelineStageTransferBit),
		vk.PipelineStageFlags(vk.PipelineStageFragmentShaderBit),
		0,
		0,
		nil,
		uint32(len(barriers)),
		barriers,
		0,
		nil,
	)
}
