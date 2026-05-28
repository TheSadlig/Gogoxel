package bindings

import (
	"testing"

	"Gogoxel/internal/input"
)

func TestForAction(t *testing.T) {
	var m Map
	m.Add(Binding{Action: input.Action("jump"), Sources: []Source{{Key: "Space"}}})
	m.Add(Binding{Action: input.Action("jump"), Sources: []Source{{Key: "Gamepad_A"}}})
	got := m.ForAction(input.Action("jump"))
	if len(got) != 2 {
		t.Fatalf("got %d sources, want 2", len(got))
	}
}
