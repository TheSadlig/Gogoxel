// Package ecs is the foundation of the Gogoxel Entity-Component-System
// runtime. See issue #6.
//
// This first slice establishes the core data model — Entity, Component
// table, System interface, and World — without committing to the
// archetype/sparse-set storage trade-off yet. Storage is a simple
// map-of-component-tables; the hot-path bench in world_test.go
// establishes a baseline that future slices must improve on.
package ecs

import "sync/atomic"

// Entity is an opaque handle to a logical game object. Zero is the
// invalid entity (Null).
type Entity uint64

// Null is the zero entity.
const Null Entity = 0

// System advances per-frame logic over a World.
type System interface {
	Update(w *World, dtSec float64)
}

// ComponentTable stores one component type keyed by entity id. The
// interface is intentionally narrow so future slices can swap to
// archetype-based layouts without changing call sites.
type ComponentTable interface {
	Remove(e Entity)
}

// World owns entities, components, and registered systems.
type World struct {
	next     atomic.Uint64
	tables   map[string]ComponentTable
	systems  []System
}

// NewWorld returns a ready-to-use World.
func NewWorld() *World {
	return &World{tables: make(map[string]ComponentTable)}
}

// Spawn allocates a new Entity id.
func (w *World) Spawn() Entity { return Entity(w.next.Add(1)) }

// RegisterTable adds a component table under a name unique within the
// world.
func (w *World) RegisterTable(name string, t ComponentTable) { w.tables[name] = t }

// Table returns the registered table by name, or nil.
func (w *World) Table(name string) ComponentTable { return w.tables[name] }

// AddSystem appends a system to the per-tick update order.
func (w *World) AddSystem(s System) { w.systems = append(w.systems, s) }

// Tick advances all registered systems by dtSec.
func (w *World) Tick(dtSec float64) {
	for _, s := range w.systems {
		s.Update(w, dtSec)
	}
}
