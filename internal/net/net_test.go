package net

import "testing"

func TestMessageKinds(t *testing.T) {
	kinds := []Kind{KindInput, KindSnapshot, KindRPC, KindAck}
	seen := make(map[Kind]bool)
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate kind: %d", k)
		}
		seen[k] = true
	}
}
