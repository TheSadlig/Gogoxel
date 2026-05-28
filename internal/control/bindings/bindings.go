// Package bindings is the foundation of the v2 input system: a
// declarative action <-> physical input map. See issue #30.
package bindings

import "Gogoxel/internal/input"

// Source describes one physical input (key + optional modifier set).
type Source struct {
	Key      string
	Mods     uint32
	IsMouse  bool
	IsAxis   bool
}

// Binding maps a semantic action to one or more physical sources.
type Binding struct {
	Action  input.Action
	Sources []Source
}

// Map is the active binding set.
type Map struct{ bindings []Binding }

// Add registers a binding.
func (m *Map) Add(b Binding) { m.bindings = append(m.bindings, b) }

// ForAction returns all sources currently bound to the given action.
func (m *Map) ForAction(a input.Action) []Source {
	var out []Source
	for _, b := range m.bindings {
		if b.Action == a {
			out = append(out, b.Sources...)
		}
	}
	return out
}

// Bindings returns the registered binding list.
func (m *Map) Bindings() []Binding { return m.bindings }
