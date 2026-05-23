package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	automationpb "Gogoxel/internal/automation/pb"
	"Gogoxel/internal/control"
	"Gogoxel/internal/input"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/session"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	automationpb.UnimplementedAutomationReadServiceServer
	automationpb.UnimplementedAutomationControlServiceServer
	automationpb.UnimplementedAutomationArtifactServiceServer

	service session.AutomationSession
}

func New(service session.AutomationSession) *Server {
	return &Server{service: service}
}

func Register(registrar grpc.ServiceRegistrar, service session.AutomationSession) *Server {
	server := New(service)
	automationpb.RegisterAutomationReadServiceServer(registrar, server)
	automationpb.RegisterAutomationControlServiceServer(registrar, server)
	automationpb.RegisterAutomationArtifactServiceServer(registrar, server)
	return server
}

func (s *Server) Health(ctx context.Context, _ *automationpb.Empty) (*automationpb.HealthResponse, error) {
	readiness, err := s.service.GetReadiness(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.HealthResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) GetCamera(ctx context.Context, _ *automationpb.Empty) (*automationpb.GetCameraResponse, error) {
	camera, err := s.service.GetCamera(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.GetCameraResponse{Camera: cameraToProto(camera)}, nil
}

func (s *Server) GetMetrics(ctx context.Context, _ *automationpb.Empty) (*automationpb.GetMetricsResponse, error) {
	metrics, err := s.service.GetMetrics(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.GetMetricsResponse{Metrics: metricsToProto(metrics)}, nil
}

func (s *Server) ResetEngine(ctx context.Context, _ *automationpb.Empty) (*automationpb.ResetEngineResponse, error) {
	if err := s.service.Reset(ctx); err != nil {
		return nil, statusError(err, nil)
	}
	readiness, err := s.service.GetReadiness(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.ResetEngineResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) LoadGenerator(ctx context.Context, request *automationpb.LoadGeneratorRequest) (*automationpb.LoadGeneratorResponse, error) {
	if request == nil || strings.TrimSpace(request.GetName()) == "" {
		return nil, invalidArgument("generator_name_required", "generator name is required")
	}
	loadRequest := session.GeneratorLoadRequest{
		Name:       request.GetName(),
		ChunkX:     int(request.GetChunkX()),
		ChunkY:     int(request.GetChunkY()),
		ChunkRange: int(request.GetChunkRange()),
	}
	if err := s.service.LoadGenerator(ctx, loadRequest); err != nil {
		return nil, statusError(err, map[string]string{
			"generator_name": request.GetName(),
			"chunk_x":        fmt.Sprintf("%d", loadRequest.ChunkX),
			"chunk_y":        fmt.Sprintf("%d", loadRequest.ChunkY),
			"chunk_range":    fmt.Sprintf("%d", loadRequest.ChunkRange),
		})
	}
	readiness, err := s.service.GetReadiness(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.LoadGeneratorResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) SetCamera(ctx context.Context, request *automationpb.SetCameraRequest) (*automationpb.SetCameraResponse, error) {
	camera, err := cameraFromProto(request.GetCamera())
	if err != nil {
		return nil, statusError(err, nil)
	}
	if err := s.service.SetCamera(ctx, camera); err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.SetCameraResponse{Camera: cameraToProto(camera)}, nil
}

func (s *Server) SetSimulationTickRate(ctx context.Context, request *automationpb.SetSimulationTickRateRequest) (*automationpb.SetSimulationTickRateResponse, error) {
	tickRateHz := int(request.GetTickRateHz())
	if tickRateHz <= 0 {
		return nil, invalidArgument("invalid_tick_rate", "tick rate must be positive")
	}
	if err := s.service.SetTickRate(ctx, tickRateHz); err != nil {
		return nil, statusError(err, map[string]string{"tick_rate_hz": fmt.Sprintf("%d", tickRateHz)})
	}
	return &automationpb.SetSimulationTickRateResponse{TickRateHz: uint32(tickRateHz)}, nil
}

func (s *Server) InjectAction(ctx context.Context, request *automationpb.InjectActionRequest) (*automationpb.InjectActionResponse, error) {
	action, err := parseAction(request.GetAction())
	if err != nil {
		return nil, statusError(err, nil)
	}
	switch request.GetState() {
	case automationpb.ActionState_ACTION_STATE_PRESS:
		err = s.service.PressAction(ctx, action)
	case automationpb.ActionState_ACTION_STATE_RELEASE:
		err = s.service.ReleaseAction(ctx, action)
	default:
		return nil, invalidArgument("invalid_action_state", "action state must be press or release")
	}
	if err != nil {
		return nil, statusError(err, map[string]string{"action": string(action)})
	}
	readiness, err := s.service.GetReadiness(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.InjectActionResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) SetSelectedMaterial(ctx context.Context, request *automationpb.SetSelectedMaterialRequest) (*automationpb.SetSelectedMaterialResponse, error) {
	name := strings.TrimSpace(request.GetMaterialName())
	if name == "" {
		return nil, invalidArgument("material_name_required", "material name is required")
	}
	if err := s.service.SetSelectedMaterial(ctx, name); err != nil {
		return nil, statusError(err, map[string]string{"material_name": name})
	}
	return &automationpb.SetSelectedMaterialResponse{MaterialName: name}, nil
}

func (s *Server) EditAtCursor(ctx context.Context, request *automationpb.EditAtCursorRequest) (*automationpb.EditAtCursorResponse, error) {
	if request == nil || request.GetCursor() == nil {
		return nil, invalidArgument("cursor_required", "cursor payload is required")
	}
	cursor := request.GetCursor()
	if cursor.GetNormalizedX() < 0 || cursor.GetNormalizedX() > 1 || cursor.GetNormalizedY() < 0 || cursor.GetNormalizedY() > 1 {
		return nil, invalidArgument("cursor_out_of_range", "cursor coordinates must be between 0 and 1")
	}
	mode, err := editModeFromProto(request.GetMode())
	if err != nil {
		return nil, statusError(err, nil)
	}
	result, err := s.service.EditAtCursor(ctx, mode, session.CursorPosition{
		NormalizedX: cursor.GetNormalizedX(),
		NormalizedY: cursor.GetNormalizedY(),
	})
	if err != nil {
		return nil, statusError(err, map[string]string{"mode": string(mode)})
	}
	return &automationpb.EditAtCursorResponse{
		Changed:      result.Changed,
		HitVoxel:     voxelToProto(result.HitVoxel),
		TargetVoxel:  voxelToProto(result.TargetVoxel),
		MaterialName: result.MaterialName,
	}, nil
}

func (s *Server) ClickUiElement(ctx context.Context, request *automationpb.ClickUiElementRequest) (*automationpb.ClickUiElementResponse, error) {
	if request == nil || strings.TrimSpace(request.GetLogicalId()) == "" {
		return nil, invalidArgument("logical_id_required", "logical id is required")
	}
	if err := s.service.ClickUI(ctx, request.GetLogicalId()); err != nil {
		return nil, statusError(err, map[string]string{"logical_id": request.GetLogicalId()})
	}
	readiness, err := s.service.GetReadiness(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.ClickUiElementResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) StepTicks(ctx context.Context, request *automationpb.StepTicksRequest) (*automationpb.StepTicksResponse, error) {
	result, err := s.service.StepTicks(ctx, int(request.GetTicks()))
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.StepTicksResponse{
		TicksExecuted:  uint32(result.Ticks),
		FramesExecuted: uint32(result.Frames),
		Metrics:        metricsToProto(result.Metrics),
	}, nil
}

func (s *Server) StepFrames(ctx context.Context, request *automationpb.StepFramesRequest) (*automationpb.StepFramesResponse, error) {
	result, err := s.service.StepFrames(ctx, int(request.GetFrames()))
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.StepFramesResponse{
		TicksExecuted:  uint32(result.Ticks),
		FramesExecuted: uint32(result.Frames),
		Metrics:        metricsToProto(result.Metrics),
	}, nil
}

func (s *Server) WaitUntilReady(ctx context.Context, request *automationpb.WaitUntilReadyRequest) (*automationpb.WaitUntilReadyResponse, error) {
	criteria := session.WaitCriteria{}
	if request != nil && request.Criteria != nil {
		criteria = session.WaitCriteria{
			RequireRenderer:         request.Criteria.GetRequireRenderer(),
			RequireSceneLoaded:      request.Criteria.GetRequireSceneLoaded(),
			RequireStreamingSettled: request.Criteria.GetRequireStreamingSettled(),
			MaxTicks:                int(request.Criteria.GetMaxTicks()),
		}
	}
	readiness, err := s.service.WaitUntilReady(ctx, criteria)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.WaitUntilReadyResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) ResetMetricsWindow(ctx context.Context, _ *automationpb.Empty) (*automationpb.ResetMetricsWindowResponse, error) {
	if err := s.service.ResetMetricsWindow(ctx); err != nil {
		return nil, statusError(err, nil)
	}
	metrics, err := s.service.GetMetrics(ctx)
	if err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.ResetMetricsWindowResponse{Metrics: metricsToProto(metrics)}, nil
}

func (s *Server) Stop(ctx context.Context, _ *automationpb.Empty) (*automationpb.StopResponse, error) {
	readiness, _ := s.service.GetReadiness(ctx)
	if err := s.service.Stop(ctx); err != nil {
		return nil, statusError(err, nil)
	}
	return &automationpb.StopResponse{Readiness: readinessToProto(readiness)}, nil
}

func (s *Server) CaptureScreenshot(ctx context.Context, request *automationpb.CaptureScreenshotRequest) (*automationpb.CaptureScreenshotResponse, error) {
	artifact, err := s.service.CaptureScreenshot(ctx, request.GetArtifactName())
	if err != nil {
		return nil, statusError(err, map[string]string{"artifact_name": request.GetArtifactName()})
	}
	return &automationpb.CaptureScreenshotResponse{Artifact: artifactToProto(artifact)}, nil
}

func (s *Server) ExportTrace(ctx context.Context, request *automationpb.ExportTraceRequest) (*automationpb.ExportTraceResponse, error) {
	artifact, err := s.service.ExportTrace(ctx, request.GetArtifactName())
	if err != nil {
		return nil, statusError(err, map[string]string{"artifact_name": request.GetArtifactName()})
	}
	return &automationpb.ExportTraceResponse{Artifact: artifactToProto(artifact)}, nil
}

func readinessToProto(readiness session.Readiness) *automationpb.Readiness {
	return &automationpb.Readiness{
		EngineInitialized:   readiness.EngineInitialized,
		RendererInitialized: readiness.RendererInitialized,
		SceneLoaded:         readiness.SceneLoaded,
		StreamingPlanned:    readiness.StreamingPlanned,
		StreamingSettled:    readiness.StreamingSettled,
	}
}

func cameraToProto(camera platform.Camera) *automationpb.Camera {
	return &automationpb.Camera{
		Position: &automationpb.Vector3{X: camera.Position[0], Y: camera.Position[1], Z: camera.Position[2]},
		YawDeg:   camera.YawDeg,
		PitchDeg: camera.PitchDeg,
		FovDeg:   camera.FovDeg,
	}
}

func cameraFromProto(camera *automationpb.Camera) (platform.Camera, error) {
	if camera == nil {
		return platform.Camera{}, invalidArgument("camera_required", "camera payload is required")
	}
	position := camera.GetPosition()
	if position == nil {
		return platform.Camera{}, invalidArgument("camera_position_required", "camera position is required")
	}
	return platform.Camera{
		Position: [3]float32{position.GetX(), position.GetY(), position.GetZ()},
		YawDeg:   camera.GetYawDeg(),
		PitchDeg: camera.GetPitchDeg(),
		FovDeg:   camera.GetFovDeg(),
	}, nil
}

func metricsToProto(metrics session.MetricsSnapshot) *automationpb.MetricsSnapshot {
	return &automationpb.MetricsSnapshot{
		Camera:                       cameraToProto(metrics.Camera),
		CurrentGenerator:             metrics.CurrentGenerator,
		RamBytes:                     metrics.RAMBytes,
		SystemRamBytes:               metrics.SystemRAMBytes,
		VramBytes:                    metrics.VRAMBytes,
		ChunkRamBytes:                metrics.ChunkRAMBytes,
		NodeCount:                    uint64(metrics.NodeCount),
		BrickCount:                   uint64(metrics.BrickCount),
		WorldSize:                    uint64(metrics.WorldSize),
		ResidentBrickCount:           uint64(metrics.ResidentBrickCount),
		StreamingDesiredReady:        metrics.StreamingDesiredReady,
		StreamingPendingDesiredCount: uint64(metrics.StreamingPendingDesiredCount),
		StreamingResidentLimit:       uint64(metrics.StreamingResidentLimit),
		StreamingUploadBudget:        uint64(metrics.StreamingUploadBudget),
		PresentMode:                  metrics.PresentMode,
		RendererDevice:               metrics.RendererDevice,
		AverageFps:                   metrics.AverageFPS,
		AverageFrameTimeMs:           metrics.AverageFrameTimeMs,
		P95FrameTimeMs:               metrics.P95FrameTimeMs,
		FrameSampleCount:             uint64(metrics.FrameSampleCount),
	}
}

func artifactToProto(artifact session.ArtifactInfo) *automationpb.Artifact {
	return &automationpb.Artifact{
		Kind:          artifact.Kind,
		RequestedName: artifact.RequestedName,
		Path:          artifact.Path,
		Format:        artifact.Format,
	}
}

func voxelToProto(voxel [3]uint32) *automationpb.VoxelCoordinates {
	return &automationpb.VoxelCoordinates{X: voxel[0], Y: voxel[1], Z: voxel[2]}
}

func editModeFromProto(mode automationpb.EditMode) (session.EditMode, error) {
	switch mode {
	case automationpb.EditMode_EDIT_MODE_PLACE:
		return session.EditModePlace, nil
	case automationpb.EditMode_EDIT_MODE_REMOVE:
		return session.EditModeRemove, nil
	default:
		return "", invalidArgument("invalid_edit_mode", "edit mode must be place or remove")
	}
}

func parseAction(name string) (input.Action, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", invalidArgument("action_required", "action name is required")
	}
	for _, action := range control.KnownActions() {
		if string(action) == trimmed {
			return action, nil
		}
	}
	return "", invalidArgument("unknown_action", "unknown action: "+trimmed)
}

func invalidArgument(reason, message string) error {
	st := status.New(codes.InvalidArgument, message)
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func statusError(err error, metadata map[string]string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	reason := "automation_failure"
	code := codes.Internal
	message := err.Error()
	switch {
	case strings.Contains(message, "unknown generator"):
		reason = "scene_source_not_found"
		code = codes.NotFound
	case strings.Contains(message, "ui automation is not implemented"):
		reason = "ui_automation_unimplemented"
		code = codes.Unimplemented
	case strings.Contains(message, "screenshot capture is not implemented"):
		reason = "screenshot_unimplemented"
		code = codes.Unimplemented
	case strings.Contains(message, "renderer is not initialized"):
		reason = "renderer_not_initialized"
		code = codes.FailedPrecondition
	case strings.Contains(message, "no scene is loaded"):
		reason = "scene_not_loaded"
		code = codes.FailedPrecondition
	case strings.Contains(message, "readiness criteria were not met"):
		reason = "readiness_not_met"
		code = codes.FailedPrecondition
	case strings.Contains(message, "step count must be non-negative"):
		reason = "invalid_step_count"
		code = codes.InvalidArgument
	case strings.Contains(message, "manual stepping requires manual automation mode"):
		reason = "manual_step_unavailable"
		code = codes.FailedPrecondition
	case strings.Contains(message, "tick rate must be positive"):
		reason = "invalid_tick_rate"
		code = codes.InvalidArgument
	}
	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{Reason: reason, Metadata: metadata}
	withDetails, detailErr := st.WithDetails(detail)
	if detailErr != nil {
		return st.Err()
	}
	return withDetails.Err()
}
