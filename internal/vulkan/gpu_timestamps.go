package vulkan

import (
	"fmt"
	"unsafe"

	vk "github.com/vulkan-go/vulkan"
)

// timestampQueriesPerFrame is the per-slot query count: one BEGIN at top of
// pipe before the render pass, one END at bottom of pipe after it.
const timestampQueriesPerFrame = 2

// gpuTimestamps owns a Vulkan timestamp QueryPool sized for one frame slot
// pair (begin/end) per in-flight frame. It computes a wall-clock GPU
// frame-time by reading the previous frame slot's queries after its fence is
// signalled, then resetting the slot's queries before recording the next
// command buffer. This pattern avoids pipeline stalls because the host only
// reads queries that have already completed.
//
// When the physical device does not expose a timestamp period, all methods
// degrade to no-ops and LastFrameMs returns 0.
type gpuTimestamps struct {
	device          vk.Device
	pool            vk.QueryPool
	periodNs        float32 // nanoseconds per timestamp tick (0 → disabled)
	frameSlotCount  int
	frameSlotPrimed []bool
	lastMs          [maxFramesInFlight]float64
}

func newGPUTimestamps(device vk.Device, periodNs float32, frameSlotCount int) (*gpuTimestamps, error) {
	if periodNs == 0 || frameSlotCount <= 0 {
		return &gpuTimestamps{periodNs: 0}, nil
	}
	info := vk.QueryPoolCreateInfo{
		SType:      vk.StructureTypeQueryPoolCreateInfo,
		QueryType:  vk.QueryTypeTimestamp,
		QueryCount: uint32(timestampQueriesPerFrame * frameSlotCount),
	}
	var pool vk.QueryPool
	if err := withPinnedValue(&pool, func() error {
		return vk.Error(vk.CreateQueryPool(device, &info, nil, &pool))
	}); err != nil {
		return nil, fmt.Errorf("creating timestamp query pool: %w", err)
	}
	// Vulkan requires queries to be reset before first use; do that on the
	// host via vkResetQueryPool when the device supports it. Older versions
	// rely on the per-frame CmdResetQueryPool that runs before each Begin.
	return &gpuTimestamps{
		device:          device,
		pool:            pool,
		periodNs:        periodNs,
		frameSlotCount:  frameSlotCount,
		frameSlotPrimed: make([]bool, frameSlotCount),
	}, nil
}

// enabled reports whether the device supports GPU timestamp queries.
func (g *gpuTimestamps) enabled() bool {
	return g != nil && g.periodNs > 0
}

// destroy releases the underlying VkQueryPool.
func (g *gpuTimestamps) destroy() {
	if g == nil || !g.enabled() {
		return
	}
	vk.DestroyQueryPool(g.device, g.pool, nil)
	g.pool = vk.QueryPool(unsafe.Pointer(nil))
	g.periodNs = 0
}

// readPrevious fetches the previous frame's begin/end timestamps for the
// given slot if they have been written at least once. Must be called after
// the in-flight fence for this slot has been waited on so the GPU work is
// complete. Resets the slot's queries on the GPU via the supplied command
// buffer is the caller's responsibility (see writeBegin).
func (g *gpuTimestamps) readPrevious(slot int) {
	if !g.enabled() || slot < 0 || slot >= g.frameSlotCount {
		return
	}
	if !g.frameSlotPrimed[slot] {
		// First time this slot is used — nothing to read yet.
		return
	}
	const stride = vk.DeviceSize(8) // 8 bytes per query, 64-bit results
	dataSize := uint(timestampQueriesPerFrame * 8)
	flags := vk.QueryResultFlags(vk.QueryResult64Bit | vk.QueryResultWaitBit)
	// Use a freshly-allocated heap slice so cgo's pointer checker accepts
	// the buffer. Pinning an embedded array on `g` triggers
	// "Go pointer to unpinned Go pointer" because the surrounding object
	// (g) is itself an unpinned Go pointer.
	scratch := make([]uint64, timestampQueriesPerFrame)
	var result vk.Result
	if err := withPinnedSlice(scratch, func() error {
		result = vk.GetQueryPoolResults(
			g.device,
			g.pool,
			uint32(slot*timestampQueriesPerFrame),
			uint32(timestampQueriesPerFrame),
			dataSize,
			unsafe.Pointer(&scratch[0]),
			stride,
			flags,
		)
		return nil
	}); err != nil {
		return
	}
	if result != vk.Success {
		// Drop this sample; common during swapchain recreation.
		return
	}
	begin := scratch[0]
	end := scratch[1]
	if end <= begin {
		g.lastMs[slot] = 0
		return
	}
	deltaTicks := end - begin
	g.lastMs[slot] = float64(deltaTicks) * float64(g.periodNs) / 1.0e6
}

// recordReset issues the per-slot query reset on the command buffer. Must be
// called once at the start of each frame's command buffer, before any
// CmdWriteTimestamp into that slot.
func (g *gpuTimestamps) recordReset(cmd vk.CommandBuffer, slot int) {
	if !g.enabled() || slot < 0 || slot >= g.frameSlotCount {
		return
	}
	vk.CmdResetQueryPool(cmd, g.pool, uint32(slot*timestampQueriesPerFrame), timestampQueriesPerFrame)
}

// writeBegin writes the BEGIN timestamp (TopOfPipe) for the given slot.
func (g *gpuTimestamps) writeBegin(cmd vk.CommandBuffer, slot int) {
	if !g.enabled() || slot < 0 || slot >= g.frameSlotCount {
		return
	}
	vk.CmdWriteTimestamp(cmd, vk.PipelineStageTopOfPipeBit, g.pool, uint32(slot*timestampQueriesPerFrame))
}

// writeEnd writes the END timestamp (BottomOfPipe) for the given slot, then
// marks the slot as primed so the next pass through this slot can read it.
func (g *gpuTimestamps) writeEnd(cmd vk.CommandBuffer, slot int) {
	if !g.enabled() || slot < 0 || slot >= g.frameSlotCount {
		return
	}
	vk.CmdWriteTimestamp(cmd, vk.PipelineStageBottomOfPipeBit, g.pool, uint32(slot*timestampQueriesPerFrame+1))
	g.frameSlotPrimed[slot] = true
}

// lastFrameMs returns the most-recent GPU frame time (ms) recorded by the
// most-recently-completed frame slot, or 0 if no sample is available yet.
func (g *gpuTimestamps) lastFrameMs() float64 {
	if !g.enabled() {
		return 0
	}
	var best float64
	for _, value := range g.lastMs {
		if value > best {
			best = value
		}
	}
	return best
}

// lastSlotMs returns the GPU frame time for the specific frame slot, or 0.
func (g *gpuTimestamps) lastSlotMs(slot int) float64 {
	if !g.enabled() || slot < 0 || slot >= g.frameSlotCount {
		return 0
	}
	return g.lastMs[slot]
}
