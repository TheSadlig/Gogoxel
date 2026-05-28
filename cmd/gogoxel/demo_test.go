package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintBriefingMentionsMinecraft(t *testing.T) {
	var buf bytes.Buffer
	printBriefing(&buf)
	for _, w := range []string{"Minecraft", "perlin", "mine", "play"} {
		if !strings.Contains(strings.ToLower(buf.String()), strings.ToLower(w)) {
			t.Fatalf("briefing missing %q\n%s", w, buf.String())
		}
	}
}
