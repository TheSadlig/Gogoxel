package automation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"Gogoxel/internal/platform"
)

type traceRecorder struct {
	events []traceEvent
}

type traceEvent struct {
	Type string         `json:"type"`
	Time time.Time      `json:"time"`
	Data map[string]any `json:"data,omitempty"`
}

func newTraceRecorder(metadata map[string]any) *traceRecorder {
	recorder := &traceRecorder{}
	recorder.record("session", metadata)
	return recorder
}

func (r *traceRecorder) record(eventType string, data map[string]any) {
	if r == nil {
		return
	}
	r.events = append(r.events, traceEvent{Type: eventType, Time: time.Now().UTC(), Data: data})
}

func (r *traceRecorder) recordCommand(name string, fields map[string]any) {
	data := map[string]any{"command": name}
	for key, value := range fields {
		data[key] = value
	}
	r.record("command", data)
}

func (r *traceRecorder) recordCamera(camera platform.Camera) {
	r.record("camera", map[string]any{
		"position": []float32{camera.Position[0], camera.Position[1], camera.Position[2]},
		"yaw_deg":  camera.YawDeg,
		"pitch_deg": camera.PitchDeg,
		"fov_deg":  camera.FovDeg,
	})
}

func (r *traceRecorder) recordFrame(sample time.Duration, metrics MetricsSnapshot) {
	r.record("frame", map[string]any{
		"frame_time_ms":              sample.Seconds() * 1000,
		"average_fps":                metrics.AverageFPS,
		"average_frame_time_ms":      metrics.AverageFrameTimeMs,
		"p95_frame_time_ms":          metrics.P95FrameTimeMs,
		"ram_bytes":                  metrics.RAMBytes,
		"vram_bytes":                 metrics.VRAMBytes,
		"current_generator":          metrics.CurrentGenerator,
		"resident_brick_count":       metrics.ResidentBrickCount,
		"streaming_pending_desired":  metrics.StreamingPendingDesiredCount,
	})
}

func (r *traceRecorder) recordArtifact(kind, requestedName, path string) {
	r.record("artifact", map[string]any{
		"kind":           kind,
		"requested_name": requestedName,
		"path":           path,
	})
}

func (r *traceRecorder) Export(path string) error {
	if r == nil {
		return fmt.Errorf("trace recorder is not initialized")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, event := range r.events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}