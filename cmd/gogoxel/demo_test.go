package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintBriefingMentionsObjective(t *testing.T) {
	var buf bytes.Buffer
	printBriefing(&buf)
	for _, w := range []string{"Build the Beacon", "Objective", "WASD"} {
		if !strings.Contains(buf.String(), w) {
			t.Fatalf("briefing missing %q\n%s", w, buf.String())
		}
	}
}
