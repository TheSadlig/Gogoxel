package game

import (
	"sort"
	"time"
)

type frameWindow struct {
	samples    []time.Duration
	gpuSamples []float64 // milliseconds
}

type frameWindowSnapshot struct {
	SampleCount         int
	AverageFrameTime    time.Duration
	P95FrameTime        time.Duration
	AverageFPS          float64
	GPUSampleCount      int
	AverageGPUFrameTime float64 // milliseconds
	P95GPUFrameTime     float64 // milliseconds
}

func (w *frameWindow) Reset() {
	if w == nil {
		return
	}
	w.samples = w.samples[:0]
	w.gpuSamples = w.gpuSamples[:0]
}

func (w *frameWindow) Record(sample time.Duration) {
	if w == nil || sample <= 0 {
		return
	}
	w.samples = append(w.samples, sample)
}

// RecordGPU records a GPU wall-clock frame time in milliseconds. Zero or
// negative values are ignored so absent timestamp data does not pollute
// percentiles.
func (w *frameWindow) RecordGPU(ms float64) {
	if w == nil || !(ms > 0) {
		return
	}
	w.gpuSamples = append(w.gpuSamples, ms)
}

func (w *frameWindow) Snapshot() frameWindowSnapshot {
	if w == nil || len(w.samples) == 0 {
		return frameWindowSnapshot{}
	}
	ordered := make([]time.Duration, len(w.samples))
	copy(ordered, w.samples)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i] < ordered[j]
	})
	var total time.Duration
	for _, sample := range ordered {
		total += sample
	}
	average := total / time.Duration(len(ordered))
	p95Index := (len(ordered)*95 - 1) / 100
	if p95Index < 0 {
		p95Index = 0
	}
	if p95Index >= len(ordered) {
		p95Index = len(ordered) - 1
	}
	averageFPS := 0.0
	if average > 0 {
		averageFPS = 1 / average.Seconds()
	}

	var avgGPU, p95GPU float64
	if len(w.gpuSamples) > 0 {
		gpuOrdered := make([]float64, len(w.gpuSamples))
		copy(gpuOrdered, w.gpuSamples)
		sort.Float64s(gpuOrdered)
		var gpuTotal float64
		for _, sample := range gpuOrdered {
			gpuTotal += sample
		}
		avgGPU = gpuTotal / float64(len(gpuOrdered))
		gpuP95Index := (len(gpuOrdered)*95 - 1) / 100
		if gpuP95Index < 0 {
			gpuP95Index = 0
		}
		if gpuP95Index >= len(gpuOrdered) {
			gpuP95Index = len(gpuOrdered) - 1
		}
		p95GPU = gpuOrdered[gpuP95Index]
	}

	return frameWindowSnapshot{
		SampleCount:         len(ordered),
		AverageFrameTime:    average,
		P95FrameTime:        ordered[p95Index],
		AverageFPS:          averageFPS,
		GPUSampleCount:      len(w.gpuSamples),
		AverageGPUFrameTime: avgGPU,
		P95GPUFrameTime:     p95GPU,
	}
}