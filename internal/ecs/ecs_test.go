package ecs

import "testing"

type countSystem struct{ ticks int }

func (c *countSystem) Update(w *World, dt float64) { c.ticks++ }

func TestWorldSpawnUnique(t *testing.T) {
	w := NewWorld()
	a, b := w.Spawn(), w.Spawn()
	if a == b || a == Null || b == Null {
		t.Fatalf("entities should be unique and non-null: %d %d", a, b)
	}
}

func TestWorldTick(t *testing.T) {
	w := NewWorld()
	s := &countSystem{}
	w.AddSystem(s)
	w.Tick(0.016)
	w.Tick(0.016)
	if s.ticks != 2 {
		t.Fatalf("system ticks = %d, want 2", s.ticks)
	}
}
