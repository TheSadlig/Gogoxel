package log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerHasComponent(t *testing.T) {
	var buf bytes.Buffer
	SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	WithComponent("engine").Info("scene loaded", slog.String("name", "perlin"))
	if !strings.Contains(buf.String(), `component=engine`) {
		t.Fatalf("expected component=engine in log; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), `name=perlin`) {
		t.Fatalf("expected name=perlin in log; got %q", buf.String())
	}
}

func TestRecoverLogsAndSwallows(t *testing.T) {
	var buf bytes.Buffer
	SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	func() {
		defer Recover(context.Background(), "worker")
		panic("boom")
	}()
	out := buf.String()
	if !strings.Contains(out, "panic recovered") {
		t.Fatalf("expected panic-recovered log; got %q", out)
	}
	if !strings.Contains(out, "component=worker") {
		t.Fatalf("expected component=worker; got %q", out)
	}
	if !strings.Contains(out, "panic=boom") {
		t.Fatalf("expected panic=boom; got %q", out)
	}
}
