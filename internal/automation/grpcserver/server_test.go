package grpcserver

import (
	"context"
	"math"
	"net"
	"testing"
	"time"

	"Gogoxel/internal/automation/client"
	"Gogoxel/internal/control"
	"Gogoxel/internal/game"
	"Gogoxel/internal/platform"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHeadlessGRPCMovementFlow(t *testing.T) {
	clientCtx := context.Background()
	grpcClient, hostDone := startServer(t, game.HostOptions{Headless: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
	defer grpcClient.Close()

	if err := grpcClient.LoadGenerator(clientCtx, "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := grpcClient.SetCamera(clientCtx, platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}); err != nil {
		t.Fatalf("SetCamera() error = %v", err)
	}
	if err := grpcClient.PressAction(clientCtx, string(control.ActionMoveForward)); err != nil {
		t.Fatalf("PressAction() error = %v", err)
	}
	if _, err := grpcClient.StepTicks(clientCtx, 20); err != nil {
		t.Fatalf("StepTicks() error = %v", err)
	}

	camera, err := grpcClient.GetCamera(clientCtx)
	if err != nil {
		t.Fatalf("GetCamera() error = %v", err)
	}
	if diff := math.Abs(float64(camera.Position[0] - 2)); diff > 0.0001 {
		t.Fatalf("camera X = %f, want 2.0", camera.Position[0])
	}

	if err := grpcClient.Stop(clientCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-hostDone; err != nil {
		t.Fatalf("host.Run() error = %v", err)
	}
}

func TestHeadlessGRPCSetTickRateRejectsZero(t *testing.T) {
	grpcClient, hostDone := startServer(t, game.HostOptions{Headless: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
	defer grpcClient.Close()

	err := grpcClient.SetTickRate(context.Background(), 0)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SetTickRate() code = %v, want %v (err=%v)", status.Code(err), codes.InvalidArgument, err)
	}

	if stopErr := grpcClient.Stop(context.Background()); stopErr != nil {
		t.Fatalf("Stop() error = %v", stopErr)
	}
	if err := <-hostDone; err != nil {
		t.Fatalf("host.Run() error = %v", err)
	}
}

func TestHeadlessGRPCCaptureScreenshotRequiresRenderer(t *testing.T) {
	grpcClient, hostDone := startServer(t, game.HostOptions{Headless: true, TickRateHz: 60, ArtifactDir: t.TempDir()})
	defer grpcClient.Close()

	if err := grpcClient.LoadGenerator(context.Background(), "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	_, err := grpcClient.CaptureScreenshot(context.Background(), "headless")
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CaptureScreenshot() code = %v, want %v (err=%v)", status.Code(err), codes.FailedPrecondition, err)
	}

	if stopErr := grpcClient.Stop(context.Background()); stopErr != nil {
		t.Fatalf("Stop() error = %v", stopErr)
	}
	if err := <-hostDone; err != nil {
		t.Fatalf("host.Run() error = %v", err)
	}
}

func TestHeadlessLiveGRPCRejectsManualStepAndContinuesRunning(t *testing.T) {
	grpcClient, hostDone := startServer(t, game.HostOptions{Headless: true, Live: true, TickRateHz: 120, ArtifactDir: t.TempDir()})
	defer grpcClient.Close()

	callCtx, cancelCall := context.WithTimeout(context.Background(), time.Second)
	defer cancelCall()
	if err := grpcClient.LoadGenerator(callCtx, "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := grpcClient.SetCamera(callCtx, platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}); err != nil {
		t.Fatalf("SetCamera() error = %v", err)
	}
	if err := grpcClient.PressAction(callCtx, string(control.ActionMoveForward)); err != nil {
		t.Fatalf("PressAction() error = %v", err)
	}

	_, err := grpcClient.StepTicks(callCtx, 1)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("StepTicks() code = %v, want %v (err=%v)", status.Code(err), codes.FailedPrecondition, err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		camera, err := grpcClient.GetCamera(callCtx)
		if err != nil {
			t.Fatalf("GetCamera() error = %v", err)
		}
		if camera.Position[0] >= 0.5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("camera X = %f, want at least 0.5 while live session is running", camera.Position[0])
		}
		time.Sleep(5 * time.Millisecond)
	}

	if stopErr := grpcClient.Stop(context.Background()); stopErr != nil {
		t.Fatalf("Stop() error = %v", stopErr)
	}
	if err := <-hostDone; err != nil {
		t.Fatalf("host.Run() error = %v", err)
	}
}

func startServer(t *testing.T, options game.HostOptions) (*client.GRPCClient, <-chan error) {
	t.Helper()
	host := game.NewHost(options)
	hostCtx, cancelHost := context.WithCancel(context.Background())
	hostDone := make(chan error, 1)
	go func() {
		hostDone <- host.Run(hostCtx)
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancelHost()
		t.Fatalf("net.Listen() error = %v", err)
	}
	grpcServer := grpc.NewServer()
	Register(grpcServer, host)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.GracefulStop()
		listener.Close()
		cancelHost()
		<-serverDone
	})

	grpcClient, err := client.Dial(context.Background(), listener.Addr().String())
	if err != nil {
		cancelHost()
		t.Fatalf("Dial() error = %v", err)
	}
	return grpcClient, hostDone
}