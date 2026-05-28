package actor

import "testing"

type bumpHP struct{}

func (bumpHP) Update(a *Actor, dt float64) { a.Health += float32(dt) }

func TestSpawnAndTick(t *testing.T) {
	m := NewManager()
	id := m.Spawn(Transform{}, 100, bumpHP{})
	if m.Get(id).Health != 100 {
		t.Fatalf("init health wrong")
	}
	m.Tick(1)
	if m.Get(id).Health != 101 {
		t.Fatalf("after tick = %v", m.Get(id).Health)
	}
}
