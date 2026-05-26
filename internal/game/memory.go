package game

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

type processMemoryStats struct {
	HeapBytes   uint64
	SystemBytes uint64
}

func currentProcessMemoryStats() processMemoryStats {
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	systemBytes := memStats.Sys
	if rssBytes, ok := linuxProcessResidentMemoryBytes(); ok {
		systemBytes = rssBytes
	}
	return processMemoryStats{
		HeapBytes:   memStats.Alloc,
		SystemBytes: systemBytes,
	}
}

// currentHeapTotalAllocBytes returns the monotonic counter of cumulative
// allocated bytes since process start (runtime.MemStats.TotalAlloc).
// Subtracting baselines yields a delta over a window.
func currentHeapTotalAllocBytes() uint64 {
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	return memStats.TotalAlloc
}

func linuxProcessResidentMemoryBytes() (uint64, bool) {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}
	residentPages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return residentPages * uint64(os.Getpagesize()), true
}