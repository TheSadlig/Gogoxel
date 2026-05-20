//go:build godog

package bdd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"Gogoxel/internal/automation"
	automationclient "Gogoxel/internal/automation/client"
	"Gogoxel/internal/platform"

	"github.com/cucumber/godog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type automationDriver interface {
	Reset(context.Context) error
	LoadGenerator(context.Context, string) error
	SetTickRate(context.Context, int) error
	SetCamera(context.Context, platform.Camera) error
	GetCamera(context.Context) (platform.Camera, error)
	PressAction(context.Context, string) error
	ReleaseAction(context.Context, string) error
	StepTicks(context.Context, int) (automation.StepResult, error)
	StepFrames(context.Context, int) (automation.StepResult, error)
	WaitUntilReady(context.Context, automation.WaitCriteria) (automation.Readiness, error)
	ResetMetricsWindow(context.Context) (automation.MetricsSnapshot, error)
	GetMetrics(context.Context) (automation.MetricsSnapshot, error)
	CaptureScreenshot(context.Context, string) (automation.ArtifactInfo, error)
	ExportTrace(context.Context, string) (automation.ArtifactInfo, error)
	Stop(context.Context) error
	Close() error
}

type sessionMode string

const (
	sessionModeHeadless     sessionMode = "headless"
	sessionModeHiddenWindow sessionMode = "hidden-window"

	defaultRPCDeadline = 10 * time.Second
	startupTimeout     = 20 * time.Second
	shutdownTimeout    = 10 * time.Second
)

var (
	binaryBuildOnce sync.Once
	builtBinaryPath string
	builtBinaryErr  error
)

type scenarioWorld struct {
	driver        automationDriver
	command       *exec.Cmd
	waitCh        chan error
	commandOutput bytes.Buffer
	artifactDir   string
	lastCamera    platform.Camera
	lastReadiness automation.Readiness
	lastMetrics   automation.MetricsSnapshot
	lastStep      automation.StepResult
	lastScreenshot automation.ArtifactInfo
	lastTrace     automation.ArtifactInfo
	mode          sessionMode
	address       string
	closed        bool
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	world := &scenarioWorld{}

	ctx.Before(func(runCtx context.Context, _ *godog.Scenario) (context.Context, error) {
		*world = scenarioWorld{}
		return runCtx, nil
	})

	ctx.After(func(runCtx context.Context, _ *godog.Scenario, stepErr error) (context.Context, error) {
		shutdownErr := world.shutdown(stepErr != nil)
		if stepErr == nil && shutdownErr != nil {
			return runCtx, shutdownErr
		}
		return runCtx, nil
	})

	ctx.Step(`^an automation session is started in headless mode$`, world.startHeadless)
	ctx.Step(`^an automation session is started in hidden-window mode$`, world.startHiddenWindow)
	ctx.Step(`^the engine is reset to a clean state$`, world.resetEngine)
	ctx.Step(`^the simulation tick rate is (\d+) Hz$`, world.setTickRate)
	ctx.Step(`^the generator "([^"]+)" is loaded$`, world.loadGenerator)
	ctx.Step(`^the camera is set to x (-?\d+(?:\.\d+)?) y (-?\d+(?:\.\d+)?) z (-?\d+(?:\.\d+)?) yaw (-?\d+(?:\.\d+)?) pitch (-?\d+(?:\.\d+)?) fov (-?\d+(?:\.\d+)?)$`, world.setCamera)
	ctx.Step(`^I hold the action "([^"]+)"$`, world.holdAction)
	ctx.Step(`^I release the action "([^"]+)"$`, world.releaseAction)
	ctx.Step(`^I advance the simulation by (\d+) ticks$`, world.advanceTicks)
	ctx.Step(`^I advance the simulation by (\d+) frames$`, world.advanceFrames)
	ctx.Step(`^the automation session becomes render-ready$`, world.waitForRenderReady)
	ctx.Step(`^I reset the metrics window$`, world.resetMetricsWindow)
	ctx.Step(`^the camera x position should be approximately (-?\d+(?:\.\d+)?) within (\d+(?:\.\d+)?)$`, world.expectCameraX)
	ctx.Step(`^the metrics window should contain at least (\d+) samples$`, world.expectMetricSamples)
	ctx.Step(`^the average FPS should be above (\d+(?:\.\d+)?)$`, world.expectAverageFPS)
	ctx.Step(`^the renderer device name should not be empty$`, world.expectRendererDevice)
	ctx.Step(`^I capture the screenshot artifact "([^"]+)"$`, world.captureScreenshot)
	ctx.Step(`^the screenshot artifact should exist$`, world.expectScreenshotArtifact)
	ctx.Step(`^I export the trace artifact "([^"]+)"$`, world.exportTrace)
	ctx.Step(`^the trace artifact should exist$`, world.expectTraceArtifact)
	ctx.Step(`^the trace artifact should contain "([^"]+)"$`, world.expectTraceContains)
}

func (w *scenarioWorld) startHeadless() error {
	return w.startSession(sessionModeHeadless)
}

func (w *scenarioWorld) startHiddenWindow() error {
	if strings.TrimSpace(os.Getenv("GOGOXEL_BDD_GPU")) == "" {
		return godog.ErrSkip
	}
	return w.startSession(sessionModeHiddenWindow)
}

func (w *scenarioWorld) startSession(mode sessionMode) error {
	if w.driver != nil {
		return fmt.Errorf("automation session is already running")
	}
	binaryPath, err := gogoxelBinaryPath()
	if err != nil {
		return err
	}
	artifactDir, err := os.MkdirTemp("", "gogoxel-bdd-artifacts-")
	if err != nil {
		return err
	}
	address, err := reserveLoopbackAddress()
	if err != nil {
		os.RemoveAll(artifactDir)
		return err
	}
	args := []string{"--automation", "--listen", address, "--artifact-dir", artifactDir}
	if mode == sessionModeHeadless {
		args = append(args, "--headless")
	} else {
		args = append(args, "--hidden-window")
	}
	command := exec.Command(binaryPath, args...)
	command.Dir = workspaceRoot()
	command.Stdout = &w.commandOutput
	command.Stderr = &w.commandOutput
	if err := command.Start(); err != nil {
		os.RemoveAll(artifactDir)
		return fmt.Errorf("starting automation session: %w", err)
	}
	w.waitCh = make(chan error, 1)
	go func() {
		w.waitCh <- command.Wait()
	}()

	driver, err := waitForDriver(address, w.waitCh, &w.commandOutput)
	if err != nil {
		_ = command.Process.Kill()
		<-w.waitCh
		os.RemoveAll(artifactDir)
		return err
	}
	w.driver = driver
	w.command = command
	w.artifactDir = artifactDir
	w.mode = mode
	w.address = address
	w.closed = false
	return nil
}

func (w *scenarioWorld) shutdown(preserveArtifacts bool) error {
	if w.closed {
		return nil
	}
	w.closed = true
	var shutdownErr error
	if w.driver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		stopErr := w.driver.Stop(ctx)
		cancel()
		if stopErr != nil {
			code := status.Code(stopErr)
			if code != codes.Canceled && code != codes.Unavailable {
				shutdownErr = fmt.Errorf("stopping automation session: %w", stopErr)
			}
		}
		if closeErr := w.driver.Close(); shutdownErr == nil && closeErr != nil {
			shutdownErr = closeErr
		}
		w.driver = nil
	}
	if w.command != nil {
		if err := w.waitForExit(); shutdownErr == nil && err != nil {
			shutdownErr = err
		}
		w.command = nil
	}
	if !preserveArtifacts && w.artifactDir != "" {
		_ = os.RemoveAll(w.artifactDir)
	}
	return shutdownErr
}

func (w *scenarioWorld) waitForExit() error {
	if w.waitCh == nil {
		return nil
	}
	select {
	case err := <-w.waitCh:
		if err != nil {
			return fmt.Errorf("automation session exited unexpectedly: %w\n%s", err, strings.TrimSpace(w.commandOutput.String()))
		}
		return nil
	case <-time.After(shutdownTimeout):
		if w.command != nil && w.command.Process != nil {
			_ = w.command.Process.Kill()
		}
		err := <-w.waitCh
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("automation session required force-kill: %w\n%s", err, strings.TrimSpace(w.commandOutput.String()))
		}
		return fmt.Errorf("automation session did not stop within %s\n%s", shutdownTimeout, strings.TrimSpace(w.commandOutput.String()))
	}
}

func (w *scenarioWorld) resetEngine() error {
	ctx, cancel := rpcContext()
	defer cancel()
	return w.driver.Reset(ctx)
}

func (w *scenarioWorld) setTickRate(rate int) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return w.driver.SetTickRate(ctx, rate)
}

func (w *scenarioWorld) loadGenerator(name string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return w.driver.LoadGenerator(ctx, name)
}

func (w *scenarioWorld) setCamera(x, y, z, yaw, pitch, fov float64) error {
	camera := platform.Camera{
		Position: [3]float32{float32(x), float32(y), float32(z)},
		YawDeg:   float32(yaw),
		PitchDeg: float32(pitch),
		FovDeg:   float32(fov),
	}
	ctx, cancel := rpcContext()
	defer cancel()
	if err := w.driver.SetCamera(ctx, camera); err != nil {
		return err
	}
	w.lastCamera = camera
	return nil
}

func (w *scenarioWorld) holdAction(action string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return w.driver.PressAction(ctx, action)
}

func (w *scenarioWorld) releaseAction(action string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return w.driver.ReleaseAction(ctx, action)
}

func (w *scenarioWorld) advanceTicks(ticks int) error {
	ctx, cancel := rpcContext()
	defer cancel()
	result, err := w.driver.StepTicks(ctx, ticks)
	if err != nil {
		return err
	}
	w.lastStep = result
	w.lastMetrics = result.Metrics
	return nil
}

func (w *scenarioWorld) advanceFrames(frames int) error {
	ctx, cancel := rpcContext()
	defer cancel()
	result, err := w.driver.StepFrames(ctx, frames)
	if err != nil {
		return err
	}
	w.lastStep = result
	w.lastMetrics = result.Metrics
	return nil
}

func (w *scenarioWorld) waitForRenderReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	readiness, err := w.driver.WaitUntilReady(ctx, automation.WaitCriteria{
		RequireRenderer:         true,
		RequireSceneLoaded:      true,
		RequireStreamingSettled: true,
		MaxTicks:                600,
	})
	if err != nil {
		return err
	}
	w.lastReadiness = readiness
	return nil
}

func (w *scenarioWorld) resetMetricsWindow() error {
	ctx, cancel := rpcContext()
	defer cancel()
	metrics, err := w.driver.ResetMetricsWindow(ctx)
	if err != nil {
		return err
	}
	w.lastMetrics = metrics
	return nil
}

func (w *scenarioWorld) expectCameraX(expected, tolerance float64) error {
	ctx, cancel := rpcContext()
	defer cancel()
	camera, err := w.driver.GetCamera(ctx)
	if err != nil {
		return err
	}
	w.lastCamera = camera
	actual := float64(camera.Position[0])
	if math.Abs(actual-expected) > tolerance {
		return fmt.Errorf("expected camera x to be %.3f +/- %.3f, got %.3f", expected, tolerance, actual)
	}
	return nil
}

func (w *scenarioWorld) expectMetricSamples(minimum int) error {
	metrics, err := w.refreshMetrics()
	if err != nil {
		return err
	}
	if metrics.FrameSampleCount < minimum {
		return fmt.Errorf("expected at least %d frame samples, got %d", minimum, metrics.FrameSampleCount)
	}
	return nil
}

func (w *scenarioWorld) expectAverageFPS(minimum float64) error {
	metrics, err := w.refreshMetrics()
	if err != nil {
		return err
	}
	if metrics.AverageFPS <= minimum {
		return fmt.Errorf("expected average FPS above %.2f, got %.2f", minimum, metrics.AverageFPS)
	}
	return nil
}

func (w *scenarioWorld) expectRendererDevice() error {
	metrics, err := w.refreshMetrics()
	if err != nil {
		return err
	}
	if strings.TrimSpace(metrics.RendererDevice) == "" {
		return fmt.Errorf("expected a non-empty renderer device name")
	}
	return nil
}

func (w *scenarioWorld) captureScreenshot(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	artifact, err := w.driver.CaptureScreenshot(ctx, name)
	if err != nil {
		return err
	}
	w.lastScreenshot = artifact
	return nil
}

func (w *scenarioWorld) expectScreenshotArtifact() error {
	if strings.TrimSpace(w.lastScreenshot.Path) == "" {
		return fmt.Errorf("no screenshot artifact has been captured")
	}
	file, err := os.Open(w.lastScreenshot.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("screenshot artifact is empty: %s", w.lastScreenshot.Path)
	}
	if _, err := png.DecodeConfig(file); err != nil {
		return fmt.Errorf("decoding screenshot artifact: %w", err)
	}
	return nil
}

func (w *scenarioWorld) exportTrace(name string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	artifact, err := w.driver.ExportTrace(ctx, name)
	if err != nil {
		return err
	}
	w.lastTrace = artifact
	return nil
}

func (w *scenarioWorld) expectTraceArtifact() error {
	if strings.TrimSpace(w.lastTrace.Path) == "" {
		return fmt.Errorf("no trace artifact has been exported")
	}
	info, err := os.Stat(w.lastTrace.Path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("trace artifact is empty: %s", w.lastTrace.Path)
	}
	return nil
}

func (w *scenarioWorld) expectTraceContains(fragment string) error {
	if err := w.expectTraceArtifact(); err != nil {
		return err
	}
	data, err := os.ReadFile(w.lastTrace.Path)
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte(fragment)) {
		return fmt.Errorf("expected trace artifact to contain %q", fragment)
	}
	return nil
}

func (w *scenarioWorld) refreshMetrics() (automation.MetricsSnapshot, error) {
	ctx, cancel := rpcContext()
	defer cancel()
	metrics, err := w.driver.GetMetrics(ctx)
	if err != nil {
		return automation.MetricsSnapshot{}, err
	}
	w.lastMetrics = metrics
	return metrics, nil
}

func waitForDriver(address string, waitCh <-chan error, output *bytes.Buffer) (automationDriver, error) {
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-waitCh:
			if err == nil {
				err = fmt.Errorf("automation session exited before gRPC became ready")
			}
			return nil, fmt.Errorf("%w\n%s", err, strings.TrimSpace(output.String()))
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		driver, err := automationclient.Dial(ctx, address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		cancel()
		if err == nil {
			readinessCtx, readinessCancel := rpcContext()
			_, readinessErr := driver.GetReadiness(readinessCtx)
			readinessCancel()
			if readinessErr == nil {
				return driver, nil
			}
			_ = driver.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("automation session did not become ready within %s\n%s", startupTimeout, strings.TrimSpace(output.String()))
}

func gogoxelBinaryPath() (string, error) {
	binaryBuildOnce.Do(func() {
		buildDir, err := os.MkdirTemp("", "gogoxel-bdd-bin-")
		if err != nil {
			builtBinaryErr = err
			return
		}
		builtBinaryPath = filepath.Join(buildDir, "gogoxel-bdd")
		command := exec.Command("go", "build", "-o", builtBinaryPath, "./cmd/gogoxel")
		command.Dir = workspaceRoot()
		output, err := command.CombinedOutput()
		if err != nil {
			builtBinaryErr = fmt.Errorf("building gogoxel automation binary: %w\n%s", err, strings.TrimSpace(string(output)))
		}
	})
	return builtBinaryPath, builtBinaryErr
}

func reserveLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

func rpcContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultRPCDeadline)
}

func workspaceRoot() string {
	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}