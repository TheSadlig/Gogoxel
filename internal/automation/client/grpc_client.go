package client

import (
	"context"

	automationpb "Gogoxel/internal/automation/pb"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/session"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	conn      *grpc.ClientConn
	read      automationpb.AutomationReadServiceClient
	control   automationpb.AutomationControlServiceClient
	artifacts automationpb.AutomationArtifactServiceClient
}

func Dial(ctx context.Context, target string, options ...grpc.DialOption) (*GRPCClient, error) {
	if len(options) == 0 {
		options = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.DialContext(ctx, target, options...)
	if err != nil {
		return nil, err
	}
	return &GRPCClient{
		conn:      conn,
		read:      automationpb.NewAutomationReadServiceClient(conn),
		control:   automationpb.NewAutomationControlServiceClient(conn),
		artifacts: automationpb.NewAutomationArtifactServiceClient(conn),
	}, nil
}

func (c *GRPCClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *GRPCClient) GetReadiness(ctx context.Context) (session.Readiness, error) {
	response, err := c.read.Health(ctx, &automationpb.Empty{})
	if err != nil {
		return session.Readiness{}, err
	}
	return readinessFromProto(response.GetReadiness()), nil
}

func (c *GRPCClient) Reset(ctx context.Context) error {
	_, err := c.control.ResetEngine(ctx, &automationpb.Empty{})
	return err
}

func (c *GRPCClient) LoadGenerator(ctx context.Context, request session.GeneratorLoadRequest) error {
	_, err := c.control.LoadGenerator(ctx, &automationpb.LoadGeneratorRequest{
		Name:       request.Name,
		ChunkX:     int32(request.ChunkX),
		ChunkY:     int32(request.ChunkY),
		ChunkRange: uint32(request.ChunkRange),
	})
	return err
}

func (c *GRPCClient) SetCamera(ctx context.Context, camera platform.Camera) error {
	_, err := c.control.SetCamera(ctx, &automationpb.SetCameraRequest{Camera: cameraToProto(camera)})
	return err
}

func (c *GRPCClient) GetCamera(ctx context.Context) (platform.Camera, error) {
	response, err := c.read.GetCamera(ctx, &automationpb.Empty{})
	if err != nil {
		return platform.Camera{}, err
	}
	return cameraFromProto(response.GetCamera()), nil
}

func (c *GRPCClient) SetTickRate(ctx context.Context, tickRateHz int) error {
	_, err := c.control.SetSimulationTickRate(ctx, &automationpb.SetSimulationTickRateRequest{TickRateHz: uint32(tickRateHz)})
	return err
}

func (c *GRPCClient) PressAction(ctx context.Context, action string) error {
	_, err := c.control.InjectAction(ctx, &automationpb.InjectActionRequest{Action: action, State: automationpb.ActionState_ACTION_STATE_PRESS})
	return err
}

func (c *GRPCClient) ReleaseAction(ctx context.Context, action string) error {
	_, err := c.control.InjectAction(ctx, &automationpb.InjectActionRequest{Action: action, State: automationpb.ActionState_ACTION_STATE_RELEASE})
	return err
}

func (c *GRPCClient) SetSelectedMaterial(ctx context.Context, name string) error {
	_, err := c.control.SetSelectedMaterial(ctx, &automationpb.SetSelectedMaterialRequest{MaterialName: name})
	return err
}

func (c *GRPCClient) EditAtCursor(ctx context.Context, mode session.EditMode, cursor session.CursorPosition) (session.CursorEditResult, error) {
	response, err := c.control.EditAtCursor(ctx, &automationpb.EditAtCursorRequest{
		Cursor: &automationpb.NormalizedCursor{
			NormalizedX: cursor.NormalizedX,
			NormalizedY: cursor.NormalizedY,
		},
		Mode: editModeToProto(mode),
	})
	if err != nil {
		return session.CursorEditResult{}, err
	}
	return session.CursorEditResult{
		Changed:      response.GetChanged(),
		HitVoxel:     voxelFromProto(response.GetHitVoxel()),
		TargetVoxel:  voxelFromProto(response.GetTargetVoxel()),
		MaterialName: response.GetMaterialName(),
	}, nil
}

func (c *GRPCClient) ClickUI(ctx context.Context, logicalID string) error {
	_, err := c.control.ClickUiElement(ctx, &automationpb.ClickUiElementRequest{LogicalId: logicalID})
	return err
}

func (c *GRPCClient) StepTicks(ctx context.Context, ticks int) (session.StepResult, error) {
	response, err := c.control.StepTicks(ctx, &automationpb.StepTicksRequest{Ticks: uint32(ticks)})
	if err != nil {
		return session.StepResult{}, err
	}
	return session.StepResult{
		Ticks:   int(response.GetTicksExecuted()),
		Frames:  int(response.GetFramesExecuted()),
		Metrics: metricsFromProto(response.GetMetrics()),
	}, nil
}

func (c *GRPCClient) StepFrames(ctx context.Context, frames int) (session.StepResult, error) {
	response, err := c.control.StepFrames(ctx, &automationpb.StepFramesRequest{Frames: uint32(frames)})
	if err != nil {
		return session.StepResult{}, err
	}
	return session.StepResult{
		Ticks:   int(response.GetTicksExecuted()),
		Frames:  int(response.GetFramesExecuted()),
		Metrics: metricsFromProto(response.GetMetrics()),
	}, nil
}

func (c *GRPCClient) WaitUntilReady(ctx context.Context, criteria session.WaitCriteria) (session.Readiness, error) {
	response, err := c.control.WaitUntilReady(ctx, &automationpb.WaitUntilReadyRequest{Criteria: &automationpb.WaitCriteria{
		RequireRenderer:         criteria.RequireRenderer,
		RequireSceneLoaded:      criteria.RequireSceneLoaded,
		RequireStreamingSettled: criteria.RequireStreamingSettled,
		MaxTicks:                uint32(criteria.MaxTicks),
	}})
	if err != nil {
		return session.Readiness{}, err
	}
	return readinessFromProto(response.GetReadiness()), nil
}

func (c *GRPCClient) ResetMetricsWindow(ctx context.Context) (session.MetricsSnapshot, error) {
	response, err := c.control.ResetMetricsWindow(ctx, &automationpb.Empty{})
	if err != nil {
		return session.MetricsSnapshot{}, err
	}
	return metricsFromProto(response.GetMetrics()), nil
}

func (c *GRPCClient) GetMetrics(ctx context.Context) (session.MetricsSnapshot, error) {
	response, err := c.read.GetMetrics(ctx, &automationpb.Empty{})
	if err != nil {
		return session.MetricsSnapshot{}, err
	}
	return metricsFromProto(response.GetMetrics()), nil
}

func (c *GRPCClient) CaptureScreenshot(ctx context.Context, name string) (session.ArtifactInfo, error) {
	response, err := c.artifacts.CaptureScreenshot(ctx, &automationpb.CaptureScreenshotRequest{ArtifactName: name})
	if err != nil {
		return session.ArtifactInfo{}, err
	}
	return artifactFromProto(response.GetArtifact()), nil
}

func (c *GRPCClient) ExportTrace(ctx context.Context, name string) (session.ArtifactInfo, error) {
	response, err := c.artifacts.ExportTrace(ctx, &automationpb.ExportTraceRequest{ArtifactName: name})
	if err != nil {
		return session.ArtifactInfo{}, err
	}
	return artifactFromProto(response.GetArtifact()), nil
}

func (c *GRPCClient) Stop(ctx context.Context) error {
	_, err := c.control.Stop(ctx, &automationpb.Empty{})
	return err
}

func readinessFromProto(readiness *automationpb.Readiness) session.Readiness {
	if readiness == nil {
		return session.Readiness{}
	}
	return session.Readiness{
		EngineInitialized:   readiness.GetEngineInitialized(),
		RendererInitialized: readiness.GetRendererInitialized(),
		SceneLoaded:         readiness.GetSceneLoaded(),
		StreamingPlanned:    readiness.GetStreamingPlanned(),
		StreamingSettled:    readiness.GetStreamingSettled(),
	}
}

func cameraFromProto(camera *automationpb.Camera) platform.Camera {
	if camera == nil || camera.GetPosition() == nil {
		return platform.Camera{}
	}
	position := camera.GetPosition()
	return platform.Camera{
		Position: [3]float32{position.GetX(), position.GetY(), position.GetZ()},
		YawDeg:   camera.GetYawDeg(),
		PitchDeg: camera.GetPitchDeg(),
		FovDeg:   camera.GetFovDeg(),
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

func metricsFromProto(metrics *automationpb.MetricsSnapshot) session.MetricsSnapshot {
	if metrics == nil {
		return session.MetricsSnapshot{}
	}
	return session.MetricsSnapshot{
		Camera:                          cameraFromProto(metrics.GetCamera()),
		CurrentGenerator:                metrics.GetCurrentGenerator(),
		RAMBytes:                        metrics.GetRamBytes(),
		SystemRAMBytes:                  metrics.GetSystemRamBytes(),
		VRAMBytes:                       metrics.GetVramBytes(),
		ChunkRAMBytes:                   metrics.GetChunkRamBytes(),
		NodeCount:                       int(metrics.GetNodeCount()),
		BrickCount:                      int(metrics.GetBrickCount()),
		WorldSize:                       uint(metrics.GetWorldSize()),
		ResidentBrickCount:              int(metrics.GetResidentBrickCount()),
		StreamingDesiredReady:           metrics.GetStreamingDesiredReady(),
		StreamingPendingDesiredCount:    int(metrics.GetStreamingPendingDesiredCount()),
		StreamingResidentLimit:          int(metrics.GetStreamingResidentLimit()),
		StreamingUploadBudget:           int(metrics.GetStreamingUploadBudget()),
		PresentMode:                     metrics.GetPresentMode(),
		RendererDevice:                  metrics.GetRendererDevice(),
		AverageFPS:                      metrics.GetAverageFps(),
		AverageFrameTimeMs:              metrics.GetAverageFrameTimeMs(),
		P95FrameTimeMs:                  metrics.GetP95FrameTimeMs(),
		FrameSampleCount:                int(metrics.GetFrameSampleCount()),
		AverageGPUFrameTimeMs:           metrics.GetAverageGpuFrameTimeMs(),
		P95GPUFrameTimeMs:               metrics.GetP95GpuFrameTimeMs(),
		GPUFrameSampleCount:             int(metrics.GetGpuFrameSampleCount()),
		HeapAllocDeltaBytes:             metrics.GetHeapAllocDeltaBytes(),
		TransferQueueUploadsWindowTotal: metrics.GetTransferQueueUploadsWindowTotal(),
		TransferUploadPath:              metrics.GetTransferUploadPath(),
		ChunkStorageStrategy:            metrics.GetChunkStorageStrategy(),
		TraversalAlgorithm:              metrics.GetTraversalAlgorithm(),
	}
}

func artifactFromProto(artifact *automationpb.Artifact) session.ArtifactInfo {
	if artifact == nil {
		return session.ArtifactInfo{}
	}
	return session.ArtifactInfo{
		Kind:          artifact.GetKind(),
		RequestedName: artifact.GetRequestedName(),
		Path:          artifact.GetPath(),
		Format:        artifact.GetFormat(),
	}
}

func editModeToProto(mode session.EditMode) automationpb.EditMode {
	switch mode {
	case session.EditModePlace:
		return automationpb.EditMode_EDIT_MODE_PLACE
	case session.EditModeRemove:
		return automationpb.EditMode_EDIT_MODE_REMOVE
	default:
		return automationpb.EditMode_EDIT_MODE_UNSPECIFIED
	}
}

func voxelFromProto(voxel *automationpb.VoxelCoordinates) [3]uint32 {
	if voxel == nil {
		return [3]uint32{}
	}
	return [3]uint32{voxel.GetX(), voxel.GetY(), voxel.GetZ()}
}
