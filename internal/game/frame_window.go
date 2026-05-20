package game

import (
	"sort"
	"time"
)

type frameWindow struct {
	samples []time.Duration
}

type frameWindowSnapshot struct {
	SampleCount      int
	AverageFrameTime time.Duration
	P95FrameTime     time.Duration
	AverageFPS       float64
}

func (w *frameWindow) Reset() {
	if w == nil {
		return
	}
	w.samples = w.samples[:0]
}

func (w *frameWindow) Record(sample time.Duration) {
	if w == nil || sample <= 0 {
		return
	}
	w.samples = append(w.samples, sample)
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
	return frameWindowSnapshot{
		SampleCount:      len(ordered),
		AverageFrameTime: average,
		P95FrameTime:     ordered[p95Index],
		AverageFPS:       averageFPS,
	}
}