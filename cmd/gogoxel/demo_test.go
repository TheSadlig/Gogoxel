package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDemoWins(t *testing.T) {
	var buf bytes.Buffer
	code := runDemo(&buf)
	if code != 0 {
		t.Fatalf("runDemo exit = %d, want 0\noutput:\n%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "win") {
		t.Fatalf("expected 'win' in output, got:\n%s", buf.String())
	}
}
