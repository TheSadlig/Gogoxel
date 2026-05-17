package input

import (
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
)

type Action string

type Binding struct {
	Key      glfw.Key
	Cooldown time.Duration
}

type KeySource interface {
	IsKeyDown(glfw.Key) bool
}

type Manager struct {
	bindings map[Action]Binding
	states   map[Action]*state
}

type state struct {
	down       bool
	triggered  bool
	nextRepeat time.Time
}

func NewManager(bindings map[Action]Binding) *Manager {
	manager := &Manager{
		bindings: make(map[Action]Binding, len(bindings)),
		states:   make(map[Action]*state, len(bindings)),
	}

	for action, binding := range bindings {
		manager.Bind(action, binding)
	}

	return manager
}

func (m *Manager) Bind(action Action, binding Binding) {
	m.bindings[action] = binding
	if _, ok := m.states[action]; !ok {
		m.states[action] = &state{}
	}
}

func (m *Manager) Update(source KeySource, now time.Time) {
	for action, binding := range m.bindings {
		current := m.stateFor(action)
		current.triggered = false

		if source == nil || !source.IsKeyDown(binding.Key) {
			current.down = false
			current.nextRepeat = time.Time{}
			continue
		}

		if !current.down {
			current.down = true
			current.triggered = true
			if binding.Cooldown > 0 {
				current.nextRepeat = now.Add(binding.Cooldown)
			}
			continue
		}

		if binding.Cooldown > 0 && !now.Before(current.nextRepeat) {
			current.triggered = true
			current.nextRepeat = now.Add(binding.Cooldown)
		}
	}
}

func (m *Manager) Down(action Action) bool {
	return m.stateFor(action).down
}

func (m *Manager) Triggered(action Action) bool {
	return m.stateFor(action).triggered
}

func (m *Manager) stateFor(action Action) *state {
	current, ok := m.states[action]
	if ok {
		return current
	}

	current = &state{}
	m.states[action] = current
	return current
}
