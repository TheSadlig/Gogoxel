package audio

import "testing"

func TestNopDriverSatisfiesDriver(t *testing.T) {
	var d Driver = NopDriver{}
	d.Begin(Listener{})
	d.Submit(Source{Gain: 1, Pitch: 1})
	d.End()
}
