package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	automationclient "Gogoxel/internal/automation/client"
	"Gogoxel/internal/control"
	"Gogoxel/internal/platform"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	startupTimeout  = 20 * time.Second
	shutdownTimeout = 10 * time.Second
	rpcTimeout      = 3 * time.Second
)

var (
	binaryBuildOnce sync.Once
	binaryPath      string
	binaryBuildErr  error
	listenPattern   = regexp.MustCompile(`automation gRPC listening on (\S+)`)
)

func TestAutomationListenStartsHeadlessLiveSession(t *testing.T) {
	process := startGogoxelProcess(t,
		"--headless",
		"--automation-listen", "127.0.0.1:0",
		"--artifact-dir", t.TempDir(),
	)
	defer process.cleanup(t)

	address, err := process.waitForListenAddress(startupTimeout)
	if err != nil {
		t.Fatalf("waitForListenAddress() error = %v", err)
	}

	grpcClient, err := dialWhenReady(process, address, startupTimeout)
	if err != nil {
		t.Fatalf("dialWhenReady() error = %v", err)
	}
	defer grpcClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	if err := grpcClient.LoadGenerator(ctx, "Cube"); err != nil {
		t.Fatalf("LoadGenerator() error = %v", err)
	}
	if err := grpcClient.SetCamera(ctx, platform.Camera{
		Position: [3]float32{0, 0, 0},
		YawDeg:   0,
		PitchDeg: 0,
		FovDeg:   60,
	}); err != nil {
		t.Fatalf("SetCamera() error = %v", err)
	}
	if err := grpcClient.PressAction(ctx, string(control.ActionMoveForward)); err != nil {
		t.Fatalf("PressAction() error = %v", err)
	}

	_, err = grpcClient.StepTicks(ctx, 1)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("StepTicks() code = %v, want %v (err=%v)", status.Code(err), codes.FailedPrecondition, err)
	}
	if reason := errorReason(err); reason != "manual_step_unavailable" {
		t.Fatalf("StepTicks() reason = %q, want %q", reason, "manual_step_unavailable")
	}

	deadline := time.Now().Add(time.Second)
	for {
		cameraCtx, cameraCancel := context.WithTimeout(context.Background(), rpcTimeout)
		camera, cameraErr := grpcClient.GetCamera(cameraCtx)
		cameraCancel()
		if cameraErr != nil {
			t.Fatalf("GetCamera() error = %v", cameraErr)
		}
		if camera.Position[0] >= 0.5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("camera X = %f, want at least 0.5 while live session is running", camera.Position[0])
		}
		time.Sleep(5 * time.Millisecond)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer stopCancel()
	if err := grpcClient.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := process.wait(shutdownTimeout); err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	process.waitConsumed = true
	process.cmd = nil
	process.waitCh = nil
	if exitErr, exited := process.pollExit(); exited {
		t.Fatalf("process exited unexpectedly after Stop wait with err=%v", exitErr)
	}
}

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(value)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type childProcess struct {
	cmd          *exec.Cmd
	output       *synchronizedBuffer
	waitCh       chan error
	waitConsumed bool
}

func startGogoxelProcess(t *testing.T, args ...string) *childProcess {
	t.Helper()
	binary := gogoxelBinaryPath(t)
	output := &synchronizedBuffer{}
	command := exec.Command(binary, args...)
	command.Dir = workspaceRoot(t)
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		t.Fatalf("command.Start() error = %v", err)
	}
	process := &childProcess{
		cmd:    command,
		output: output,
		waitCh: make(chan error, 1),
	}
	go func() {
		process.waitCh <- command.Wait()
	}()
	return process
}

func (p *childProcess) waitForListenAddress(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if match := listenPattern.FindStringSubmatch(p.output.String()); len(match) == 2 {
			return match[1], nil
		}
		if err, exited := p.pollExit(); exited {
			return "", fmt.Errorf("gogoxel process exited before reporting listen address: %w\n%s", err, strings.TrimSpace(p.output.String()))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", fmt.Errorf("gogoxel process did not report gRPC listen address within %s\n%s", timeout, strings.TrimSpace(p.output.String()))
}

func (p *childProcess) pollExit() (error, bool) {
	if p == nil || p.waitConsumed || p.waitCh == nil {
		return nil, false
	}
	select {
	case err := <-p.waitCh:
		p.waitConsumed = true
		return err, true
	default:
		return nil, false
	}
}

func (p *childProcess) wait(timeout time.Duration) error {
	if p == nil || p.waitConsumed || p.waitCh == nil {
		return nil
	}
	select {
	case err := <-p.waitCh:
		p.waitConsumed = true
		return err
	case <-time.After(timeout):
		return fmt.Errorf("gogoxel process did not exit within %s\n%s", timeout, strings.TrimSpace(p.output.String()))
	}
}

func (p *childProcess) cleanup(t *testing.T) {
	t.Helper()
	if p == nil || p.cmd == nil {
		return
	}
	if p.waitConsumed {
		return
	}
	if p.cmd.Process != nil {
		if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("Process.Kill() error = %v", err)
		}
	}
	if err := p.wait(shutdownTimeout); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("wait() after cleanup error = %v", err)
	}
	if p.cmd.ProcessState != nil && p.cmd.ProcessState.Success() {
		return
	}
	if output := strings.TrimSpace(p.output.String()); output != "" {
		_ = output
	}
	if p.cmd.ProcessState != nil && p.cmd.ProcessState.ExitCode() == -1 {
		return
	}
}

func dialWhenReady(process *childProcess, address string, timeout time.Duration) (*automationclient.GRPCClient, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err, exited := process.pollExit(); exited {
			return nil, fmt.Errorf("gogoxel process exited before gRPC became ready: %w\n%s", err, strings.TrimSpace(process.output.String()))
		}
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		grpcClient, err := automationclient.Dial(
			dialCtx,
			address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		dialCancel()
		if err == nil {
			readinessCtx, readinessCancel := context.WithTimeout(context.Background(), rpcTimeout)
			_, readinessErr := grpcClient.GetReadiness(readinessCtx)
			readinessCancel()
			if readinessErr == nil {
				return grpcClient, nil
			}
			_ = grpcClient.Close()
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("gogoxel gRPC endpoint did not become ready within %s\n%s", timeout, strings.TrimSpace(process.output.String()))
}

func errorReason(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info.Reason
		}
	}
	return ""
}

func gogoxelBinaryPath(t *testing.T) string {
	t.Helper()
	binaryBuildOnce.Do(func() {
		buildDir, err := os.MkdirTemp("", "gogoxel-cmd-test-")
		if err != nil {
			binaryBuildErr = err
			return
		}
		binaryPath = filepath.Join(buildDir, "gogoxel-test")
		command := exec.Command("go", "build", "-o", binaryPath, "./cmd/gogoxel")
		command.Dir = workspaceRoot(t)
		output, err := command.CombinedOutput()
		if err != nil {
			binaryBuildErr = fmt.Errorf("building gogoxel binary: %w\n%s", err, strings.TrimSpace(string(output)))
		}
	})
	if binaryBuildErr != nil {
		t.Fatal(binaryBuildErr)
	}
	return binaryPath
}

func workspaceRoot(t *testing.T) string {
	t.Helper()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	return filepath.Clean(filepath.Join(workingDir, "../.."))
}