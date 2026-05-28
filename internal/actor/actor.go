// Package actor is the foundation of the actor/NPC system. See issue
// #31. Actors are higher-level than raw ECS entities — they bundle a
// transform, health, and an AI controller behind a stable handle.
package actor

// ID is an opaque actor handle.
type ID uint64

// Transform is the actor's world-space pose.
type Transform struct {
	Position [3]float32
	YawPitch [2]float32
}

// Actor is the runtime state for one game actor.
type Actor struct {
	ID        ID
	Transform Transform
	Health    float32
	Tags      uint32 // bitmask
}

// Controller drives an Actor's per-tick behavior.
type Controller interface {
	Update(a *Actor, dtSec float64)
}

// Manager owns all live actors keyed by ID.
type Manager struct {
	next    ID
	actors  map[ID]*Actor
	ctrls   map[ID]Controller
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{actors: make(map[ID]*Actor), ctrls: make(map[ID]Controller)}
}

// Spawn registers a new Actor and returns its ID.
func (m *Manager) Spawn(t Transform, hp float32, c Controller) ID {
	m.next++
	id := m.next
	m.actors[id] = &Actor{ID: id, Transform: t, Health: hp}
	if c != nil {
		m.ctrls[id] = c
	}
	return id
}

// Get returns the live Actor or nil.
func (m *Manager) Get(id ID) *Actor { return m.actors[id] }

// Tick advances all actors with controllers.
func (m *Manager) Tick(dtSec float64) {
	for id, c := range m.ctrls {
		c.Update(m.actors[id], dtSec)
	}
}
