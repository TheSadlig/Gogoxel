package input

import (
	"testing"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
)

type fakeSource struct {
	keys map[glfw.Key]bool
}

func (f fakeSource) IsKeyDown(key glfw.Key) bool {
	return f.keys[key]
}

func TestTriggeredOnlyOnceWithoutCooldown(t *testing.T) {
	manager := NewManager(map[Action]Binding{
		"move": {Key: glfw.KeyW},
	})
	source := fakeSource{keys: map[glfw.Key]bool{glfw.KeyW: true}}
	now := time.Unix(10, 0)

	manager.Update(source, now)
	if !manager.Triggered("move") {
		t.Fatal("expected first press to trigger")
	}

	manager.Update(source, now.Add(10*time.Millisecond))
	if manager.Triggered("move") {
		t.Fatal("expected held key without cooldown to stop retriggering")
	}

	if !manager.Down("move") {
		t.Fatal("expected held key to remain down")
	}
}

func TestTriggeredRepeatsWithCooldown(t *testing.T) {
	manager := NewManager(map[Action]Binding{
		"move": {Key: glfw.KeyW, Cooldown: 100 * time.Millisecond},
	})
	source := fakeSource{keys: map[glfw.Key]bool{glfw.KeyW: true}}
	now := time.Unix(10, 0)

	manager.Update(source, now)
	if !manager.Triggered("move") {
		t.Fatal("expected first press to trigger")
	}

	manager.Update(source, now.Add(90*time.Millisecond))
	if manager.Triggered("move") {
		t.Fatal("expected cooldown to suppress early repeat")
	}

	manager.Update(source, now.Add(100*time.Millisecond))
	if !manager.Triggered("move") {
		t.Fatal("expected cooldown expiry to retrigger held key")
	}
}

func TestReleaseResetsTriggerState(t *testing.T) {
	manager := NewManager(map[Action]Binding{
		"move": {Key: glfw.KeyW, Cooldown: 100 * time.Millisecond},
	})
	now := time.Unix(10, 0)

	manager.Update(fakeSource{keys: map[glfw.Key]bool{glfw.KeyW: true}}, now)
	manager.Update(fakeSource{keys: map[glfw.Key]bool{}}, now.Add(10*time.Millisecond))
	if manager.Down("move") {
		t.Fatal("expected released key to clear down state")
	}

	manager.Update(fakeSource{keys: map[glfw.Key]bool{glfw.KeyW: true}}, now.Add(20*time.Millisecond))
	if !manager.Triggered("move") {
		t.Fatal("expected new press after release to trigger immediately")
	}
}
