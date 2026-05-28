package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestDoctorPrintsReport just verifies the doctor subcommand runs and
// emits the expected header + at least the go-runtime check, regardless
// of what the host environment has installed.
func TestDoctorPrintsReport(t *testing.T) {
	var buf bytes.Buffer
	_ = runDoctor(&buf)
	out := buf.String()
	for _, expect := range []string{"gogoxel doctor", "CHECK", "go-runtime", "vulkan-loader", "glslangValidator"} {
		if !strings.Contains(out, expect) {
			t.Errorf("expected doctor report to contain %q, got:\n%s", expect, out)
		}
	}
}
