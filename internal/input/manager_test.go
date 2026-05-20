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

func TestUpdateSnapshotReleasesMissingActions(t *testing.T) {
	manager := NewManager(map[Action]Binding{
		"move": {Cooldown: 100 * time.Millisecond},
	})
	now := time.Unix(10, 0)

	manager.UpdateSnapshot(Snapshot{"move": true}, now)
	if !manager.Down("move") {
		t.Fatal("expected snapshot press to mark action down")
	}
	if !manager.Triggered("move") {
		t.Fatal("expected snapshot press to trigger action")
	}

	manager.UpdateSnapshot(Snapshot{}, now.Add(10*time.Millisecond))
	if manager.Down("move") {
		t.Fatal("expected missing action in snapshot to release")
	}
	if manager.Triggered("move") {
		t.Fatal("expected release snapshot to clear triggered state")
	}
}

func TestSetActionDownSupportsUnboundActions(t *testing.T) {
	manager := NewManager(nil)
	now := time.Unix(10, 0)

	manager.SetActionDown("custom", true, now)
	if !manager.Down("custom") {
		t.Fatal("expected manual press to mark custom action down")
	}
	if !manager.Triggered("custom") {
		t.Fatal("expected manual press to trigger custom action")
	}

	manager.SetActionDown("custom", true, now.Add(10*time.Millisecond))
	if manager.Triggered("custom") {
		t.Fatal("expected held custom action without cooldown to stop retriggering")
	}

	manager.SetActionDown("custom", false, now.Add(20*time.Millisecond))
	if manager.Down("custom") {
		t.Fatal("expected manual release to clear custom action")
	}
}
