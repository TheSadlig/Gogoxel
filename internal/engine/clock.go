package engine

import "time"

type Clock interface {
	Now() time.Time
	Advance(time.Duration)
}

type ManualClock struct {
	now time.Time
}

func NewManualClock(start time.Time) *ManualClock {
	if start.IsZero() {
		start = time.Unix(0, 0)
	}
	return &ManualClock{now: start}
}

func (c *ManualClock) Now() time.Time {
	if c == nil {
		return time.Unix(0, 0)
	}
	return c.now
}

func (c *ManualClock) Advance(delta time.Duration) {
	if c == nil {
		return
	}
	c.now = c.now.Add(delta)
}