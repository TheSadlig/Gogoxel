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

	automationclient "Gogoxel/internal/automation/client"
	"Gogoxel/internal/platform"
	"Gogoxel/internal/session"

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
	StepTicks(context.Context, int) (session.StepResult, error)
	StepFrames(context.Context, int) (session.StepResult, error)
	WaitUntilReady(context.Context, session.WaitCriteria) (session.Readiness, error)
	ResetMetricsWindow(context.Context) (session.MetricsSnapshot, error)
	GetMetrics(context.Context) (session.MetricsSnapshot, error)
	CaptureScreenshot(context.Context, string) (session.ArtifactInfo, error)
	ExportTrace(context.Context, string) (session.ArtifactInfo, error)
	Stop(context.Context) error
	Close() error
}

type startupResult struct {
	driver       automationDriver
	waitConsumed bool
	err          error
}

type sessionMode string
type sessionRunMode string

const (
	sessionModeHeadless     sessionMode = "headless"
	sessionModeHiddenWindow sessionMode = "hidden-window"
	sessionRunModeManual    sessionRunMode = "manual"
	sessionRunModeLive      sessionRunMode = "live"

	defaultRPCDeadline = 10 * time.Second
	generatorLoadTimeout = 60 * time.Second
	startupTimeout     = 20 * time.Second
	shutdownTimeout    = 10 * time.Second
)

var (
	binaryBuildOnce sync.Once
	builtBinaryPath string
	builtBinaryErr  error
)

type scenarioHarness struct {
	driver         automationDriver
	command        *exec.Cmd
	waitCh         chan error
	commandOutput  bytes.Buffer
	artifactDir    string
	artifactSubdir string
	lastCamera     platform.Camera
	lastReadiness  session.Readiness
	lastMetrics    session.MetricsSnapshot
	lastStep       session.StepResult
	lastScreenshot session.ArtifactInfo
	lastTrace      session.ArtifactInfo
	mode           sessionMode
	runMode        sessionRunMode
	address        string
	cleanupArtifacts bool
	autoTraceName  string
	closed         bool
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	harness := &scenarioHarness{}

	ctx.Before(func(runCtx context.Context, scenario *godog.Scenario) (context.Context, error) {
		*harness = scenarioHarness{}
		harness.artifactSubdir = scenarioArtifactSubdir(scenario)
		harness.autoTraceName = traceNameForScenario(scenario)
		return runCtx, nil
	})

	ctx.After(func(runCtx context.Context, _ *godog.Scenario, stepErr error) (context.Context, error) {
		shutdownErr := harness.shutdown(stepErr != nil)
		if stepErr == nil && shutdownErr != nil {
			return runCtx, shutdownErr
		}
		return runCtx, nil
	})

	ctx.Step(`^an automation session is started in headless mode$`, harness.startHeadless)
	ctx.Step(`^an automation session is started in hidden-window mode$`, harness.startHiddenWindow)
	ctx.Step(`^a live automation session is started in headless mode$`, harness.startLiveHeadless)
	ctx.Step(`^the engine is reset to a clean state$`, harness.resetEngine)
	ctx.Step(`^the simulation tick rate is (\d+) Hz$`, harness.setTickRate)
	ctx.Step(`^the generator "([^"]+)" is loaded$`, harness.loadGenerator)
	ctx.Step(`^the camera is set to x (-?\d+(?:\.\d+)?) y (-?\d+(?:\.\d+)?) z (-?\d+(?:\.\d+)?) yaw (-?\d+(?:\.\d+)?) pitch (-?\d+(?:\.\d+)?) fov (-?\d+(?:\.\d+)?)$`, harness.setCamera)
	ctx.Step(`^I hold the action "([^"]+)"$`, harness.holdAction)
	ctx.Step(`^I release the action "([^"]+)"$`, harness.releaseAction)
	ctx.Step(`^I advance the simulation by (\d+) ticks$`, harness.advanceTicks)
	ctx.Step(`^I advance the simulation by (\d+) frames$`, harness.advanceFrames)
	ctx.Step(`^the automation session becomes render-ready$`, harness.waitForRendererReady)
	ctx.Step(`^I reset the metrics window$`, harness.resetMetricsWindow)
	ctx.Step(`^the camera x position should be approximately (-?\d+(?:\.\d+)?) within (\d+(?:\.\d+)?)$`, harness.expectCameraX)
	ctx.Step(`^the camera x position should eventually be above (-?\d+(?:\.\d+)?) within (\d+(?:\.\d+)?) seconds$`, harness.expectCameraXEventuallyAbove)
	ctx.Step(`^the metrics window should contain at least (\d+) samples$`, harness.expectMetricSamples)
	ctx.Step(`^the average FPS should be above (\d+(?:\.\d+)?)$`, harness.expectAverageFPS)
	ctx.Step(`^the renderer device name should not be empty$`, harness.expectRendererDevice)
	ctx.Step(`^I capture the screenshot artifact "([^"]+)"$`, harness.captureScreenshot)
	ctx.Step(`^the screenshot artifact should exist$`, harness.expectScreenshotArtifact)
	ctx.Step(`^I export the trace artifact "([^"]+)"$`, harness.exportTrace)
	ctx.Step(`^the trace artifact should exist$`, harness.expectTraceArtifact)
	ctx.Step(`^the trace artifact should contain "([^"]+)"$`, harness.expectTraceContains)
}

func (h *scenarioHarness) startHeadless() error {
	return h.startSession(sessionModeHeadless, sessionRunModeManual)
}

func (h *scenarioHarness) startHiddenWindow() error {
	if strings.TrimSpace(os.Getenv("GOGOXEL_BDD_GPU")) == "" {
		return godog.ErrSkip
	}
	return h.startSession(sessionModeHiddenWindow, sessionRunModeManual)
}

func (h *scenarioHarness) startLiveHeadless() error {
	return h.startSession(sessionModeHeadless, sessionRunModeLive)
}

func (h *scenarioHarness) startSession(mode sessionMode, runMode sessionRunMode) error {
	if h.driver != nil {
		return fmt.Errorf("automation session is already running")
	}
	binaryPath, err := gogoxelBinaryPath()
	if err != nil {
		return err
	}
	artifactDir, cleanupArtifacts, err := scenarioArtifactDir(h.artifactSubdir)
	if err != nil {
		return err
	}
	address, err := reserveLoopbackAddress()
	if err != nil {
		cleanupScenarioArtifacts(artifactDir, cleanupArtifacts)
		return err
	}
	args := []string{"--artifact-dir", artifactDir}
	switch runMode {
	case sessionRunModeManual:
		args = append(args, "--automation", "--listen", address)
	case sessionRunModeLive:
		args = append(args, "--automation-listen", address)
	default:
		cleanupScenarioArtifacts(artifactDir, cleanupArtifacts)
		return fmt.Errorf("unsupported automation run mode %q", runMode)
	}
	switch mode {
	case sessionModeHeadless:
		args = append(args, "--headless")
	case sessionModeHiddenWindow:
		args = append(args, "--hidden-window")
	default:
		cleanupScenarioArtifacts(artifactDir, cleanupArtifacts)
		return fmt.Errorf("unsupported automation session mode %q", mode)
	}
	command := exec.Command(binaryPath, args...)
	command.Dir = workspaceRoot()
	command.Stdout = &h.commandOutput
	command.Stderr = &h.commandOutput
	if err := command.Start(); err != nil {
		cleanupScenarioArtifacts(artifactDir, cleanupArtifacts)
		return fmt.Errorf("starting automation session: %w", err)
	}
	h.waitCh = make(chan error, 1)
	go func() {
		h.waitCh <- command.Wait()
	}()

	startup := waitForDriver(address, h.waitCh, &h.commandOutput)
	if startup.err != nil {
		cleanupErr := h.stopStartupCommand(command, startup.waitConsumed)
		cleanupScenarioArtifacts(artifactDir, cleanupArtifacts)
		if cleanupErr != nil {
			return errors.Join(startup.err, cleanupErr)
		}
		return startup.err
	}
	h.driver = startup.driver
	h.command = command
	h.artifactDir = artifactDir
	h.mode = mode
	h.runMode = runMode
	h.address = address
	h.cleanupArtifacts = cleanupArtifacts
	h.closed = false
	return nil
}

func (h *scenarioHarness) stopStartupCommand(command *exec.Cmd, waitConsumed bool) error {
	if waitConsumed || command == nil || command.Process == nil {
		return nil
	}
	if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("killing automation session: %w", err)
	}
	select {
	case err := <-h.waitCh:
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("waiting for automation session after startup failure: %w\n%s", err, strings.TrimSpace(h.commandOutput.String()))
		}
		return nil
	case <-time.After(shutdownTimeout):
		return fmt.Errorf("automation session did not stop within %s\n%s", shutdownTimeout, strings.TrimSpace(h.commandOutput.String()))
	}
}

func (h *scenarioHarness) shutdown(preserveArtifacts bool) error {
	if h.closed {
		return nil
	}
	h.closed = true
	var shutdownErr error
	if h.driver != nil && h.autoTraceName != "" && h.lastTrace.Path == "" {
		ctx, cancel := rpcContext()
		artifact, err := h.driver.ExportTrace(ctx, h.autoTraceName)
		cancel()
		if err == nil {
			h.lastTrace = artifact
		}
		if err != nil && shutdownErr == nil {
			shutdownErr = fmt.Errorf("exporting scenario trace: %w", err)
		}
	}
	if h.driver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		stopErr := h.driver.Stop(ctx)
		cancel()
		if stopErr != nil {
			code := status.Code(stopErr)
			if code != codes.Canceled && code != codes.Unavailable {
				shutdownErr = fmt.Errorf("stopping automation session: %w", stopErr)
			}
		}
		if closeErr := h.driver.Close(); shutdownErr == nil && closeErr != nil {
			shutdownErr = closeErr
		}
		h.driver = nil
	}
	if h.command != nil {
		if err := h.waitForExit(); shutdownErr == nil && err != nil {
			shutdownErr = err
		}
		h.command = nil
	}
	if !preserveArtifacts && h.cleanupArtifacts && h.artifactDir != "" {
		_ = os.RemoveAll(h.artifactDir)
	}
	return shutdownErr
}

func (h *scenarioHarness) waitForExit() error {
	if h.waitCh == nil {
		return nil
	}
	select {
	case err := <-h.waitCh:
		if err != nil {
			return fmt.Errorf("automation session exited unexpectedly: %w\n%s", err, strings.TrimSpace(h.commandOutput.String()))
		}
		return nil
	case <-time.After(shutdownTimeout):
		if h.command != nil && h.command.Process != nil {
			_ = h.command.Process.Kill()
		}
		err := <-h.waitCh
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("automation session required force-kill: %w\n%s", err, strings.TrimSpace(h.commandOutput.String()))
		}
		return fmt.Errorf("automation session did not stop within %s\n%s", shutdownTimeout, strings.TrimSpace(h.commandOutput.String()))
	}
}

func (h *scenarioHarness) resetEngine() error {
	ctx, cancel := rpcContext()
	defer cancel()
	return h.driver.Reset(ctx)
}

func (h *scenarioHarness) setTickRate(rate int) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return h.driver.SetTickRate(ctx, rate)
}

func (h *scenarioHarness) loadGenerator(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), generatorLoadTimeout)
	defer cancel()
	return h.driver.LoadGenerator(ctx, name)
}

func (h *scenarioHarness) setCamera(x, y, z, yaw, pitch, fov float64) error {
	camera := platform.Camera{
		Position: [3]float32{float32(x), float32(y), float32(z)},
		YawDeg:   float32(yaw),
		PitchDeg: float32(pitch),
		FovDeg:   float32(fov),
	}
	ctx, cancel := rpcContext()
	defer cancel()
	if err := h.driver.SetCamera(ctx, camera); err != nil {
		return err
	}
	h.lastCamera = camera
	return nil
}

func (h *scenarioHarness) holdAction(action string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return h.driver.PressAction(ctx, action)
}

func (h *scenarioHarness) releaseAction(action string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	return h.driver.ReleaseAction(ctx, action)
}

func (h *scenarioHarness) advanceTicks(ticks int) error {
	if h.runMode == sessionRunModeLive {
		return fmt.Errorf("advancing the simulation by ticks is only available in manual automation sessions")
	}
	ctx, cancel := rpcContext()
	defer cancel()
	result, err := h.driver.StepTicks(ctx, ticks)
	if err != nil {
		return err
	}
	h.lastStep = result
	h.lastMetrics = result.Metrics
	return nil
}

func (h *scenarioHarness) advanceFrames(frames int) error {
	if h.runMode == sessionRunModeLive {
		return fmt.Errorf("advancing the simulation by frames is only available in manual automation sessions")
	}
	ctx, cancel := rpcContext()
	defer cancel()
	result, err := h.driver.StepFrames(ctx, frames)
	if err != nil {
		return err
	}
	h.lastStep = result
	h.lastMetrics = result.Metrics
	return nil
}

func (h *scenarioHarness) waitForRendererReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	readiness, err := h.driver.WaitUntilReady(ctx, session.WaitCriteria{
		RequireRenderer:    true,
		RequireSceneLoaded: true,
		MaxTicks:           600,
	})
	if err != nil {
		return err
	}
	h.lastReadiness = readiness
	return nil
}

func (h *scenarioHarness) resetMetricsWindow() error {
	ctx, cancel := rpcContext()
	defer cancel()
	metrics, err := h.driver.ResetMetricsWindow(ctx)
	if err != nil {
		return err
	}
	h.lastMetrics = metrics
	return nil
}

func (h *scenarioHarness) expectCameraX(expected, tolerance float64) error {
	ctx, cancel := rpcContext()
	defer cancel()
	camera, err := h.driver.GetCamera(ctx)
	if err != nil {
		return err
	}
	h.lastCamera = camera
	actual := float64(camera.Position[0])
	if math.Abs(actual-expected) > tolerance {
		return fmt.Errorf("expected camera x to be %.3f +/- %.3f, got %.3f", expected, tolerance, actual)
	}
	return nil
}

func (h *scenarioHarness) expectCameraXEventuallyAbove(minimum, withinSeconds float64) error {
	if withinSeconds <= 0 {
		return fmt.Errorf("withinSeconds must be positive")
	}
	deadline := time.Now().Add(time.Duration(withinSeconds * float64(time.Second)))
	for {
		ctx, cancel := rpcContext()
		camera, err := h.driver.GetCamera(ctx)
		cancel()
		if err != nil {
			return err
		}
		h.lastCamera = camera
		actual := float64(camera.Position[0])
		if actual > minimum {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected camera x to eventually exceed %.3f within %.3f seconds, got %.3f", minimum, withinSeconds, actual)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (h *scenarioHarness) expectMetricSamples(minimum int) error {
	metrics, err := h.refreshMetrics()
	if err != nil {
		return err
	}
	if metrics.FrameSampleCount < minimum {
		return fmt.Errorf("expected at least %d frame samples, got %d", minimum, metrics.FrameSampleCount)
	}
	return nil
}

func (h *scenarioHarness) expectAverageFPS(minimum float64) error {
	metrics, err := h.refreshMetrics()
	if err != nil {
		return err
	}
	if metrics.AverageFPS <= minimum {
		return fmt.Errorf("expected average FPS above %.2f, got %.2f", minimum, metrics.AverageFPS)
	}
	return nil
}

func (h *scenarioHarness) expectRendererDevice() error {
	metrics, err := h.refreshMetrics()
	if err != nil {
		return err
	}
	if strings.TrimSpace(metrics.RendererDevice) == "" {
		return fmt.Errorf("expected a non-empty renderer device name")
	}
	return nil
}

func (h *scenarioHarness) captureScreenshot(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	artifact, err := h.driver.CaptureScreenshot(ctx, name)
	if err != nil {
		return err
	}
	h.lastScreenshot = artifact
	return nil
}

func (h *scenarioHarness) expectScreenshotArtifact() error {
	if strings.TrimSpace(h.lastScreenshot.Path) == "" {
		return fmt.Errorf("no screenshot artifact has been captured")
	}
	file, err := os.Open(h.lastScreenshot.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("screenshot artifact is empty: %s", h.lastScreenshot.Path)
	}
	if _, err := png.DecodeConfig(file); err != nil {
		return fmt.Errorf("decoding screenshot artifact: %w", err)
	}
	return nil
}

func (h *scenarioHarness) exportTrace(name string) error {
	ctx, cancel := rpcContext()
	defer cancel()
	artifact, err := h.driver.ExportTrace(ctx, name)
	if err != nil {
		return err
	}
	h.lastTrace = artifact
	return nil
}

func (h *scenarioHarness) expectTraceArtifact() error {
	if strings.TrimSpace(h.lastTrace.Path) == "" {
		return fmt.Errorf("no trace artifact has been exported")
	}
	info, err := os.Stat(h.lastTrace.Path)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("trace artifact is empty: %s", h.lastTrace.Path)
	}
	return nil
}

func (h *scenarioHarness) expectTraceContains(fragment string) error {
	if err := h.expectTraceArtifact(); err != nil {
		return err
	}
	data, err := os.ReadFile(h.lastTrace.Path)
	if err != nil {
		return err
	}
	if !bytes.Contains(data, []byte(fragment)) {
		return fmt.Errorf("expected trace artifact to contain %q", fragment)
	}
	return nil
}

func (h *scenarioHarness) refreshMetrics() (session.MetricsSnapshot, error) {
	ctx, cancel := rpcContext()
	defer cancel()
	metrics, err := h.driver.GetMetrics(ctx)
	if err != nil {
		return session.MetricsSnapshot{}, err
	}
	h.lastMetrics = metrics
	return metrics, nil
}

func waitForDriver(address string, waitCh <-chan error, output *bytes.Buffer) startupResult {
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-waitCh:
			if err == nil {
				err = fmt.Errorf("automation session exited before gRPC became ready")
			}
			return startupResult{
				waitConsumed: true,
				err:          fmt.Errorf("%w\n%s", err, strings.TrimSpace(output.String())),
			}
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
				return startupResult{driver: driver}
			}
			_ = driver.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return startupResult{
		err: fmt.Errorf("automation session did not become ready within %s\n%s", startupTimeout, strings.TrimSpace(output.String())),
	}
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

func scenarioArtifactDir(subdir string) (string, bool, error) {
	subdir = sanitizeArtifactBaseName(subdir)
	artifactRoot := strings.TrimSpace(os.Getenv("GOGOXEL_BDD_ARTIFACT_DIR"))
	if artifactRoot == "" {
		dir, err := os.MkdirTemp("", fmt.Sprintf("gogoxel-bdd-artifacts-%s-", subdir))
		return dir, true, err
	}
	if !filepath.IsAbs(artifactRoot) {
		artifactRoot = filepath.Join(workspaceRoot(), artifactRoot)
	}
	artifactDir := filepath.Join(artifactRoot, subdir)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", false, err
	}
	return artifactDir, false, nil
}

func cleanupScenarioArtifacts(dir string, cleanup bool) {
	if cleanup && dir != "" {
		_ = os.RemoveAll(dir)
	}
}

func scenarioArtifactSubdir(scenario *godog.Scenario) string {
	if scenario == nil {
		return "scenario"
	}
	return sanitizeArtifactBaseName(scenario.Name)
}

func traceNameForScenario(scenario *godog.Scenario) string {
	if scenario == nil || !hasScenarioTag(scenario, "@trace") {
		return ""
	}
	return sanitizeArtifactBaseName(scenario.Name)
}

func hasScenarioTag(scenario *godog.Scenario, target string) bool {
	if scenario == nil {
		return false
	}
	for _, tag := range scenario.Tags {
		if tag != nil && tag.Name == target {
			return true
		}
	}
	return false
}

func sanitizeArtifactBaseName(value string) string {
	name := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		" ", "-",
		"/", "-",
		"\\", "-",
		":", "-",
		"\t", "-",
		"\n", "-",
	)
	name = replacer.Replace(name)
	var builder strings.Builder
	lastDash := false
	for _, r := range name {
		isLetter := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if isLetter || isDigit {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' || r == '_' {
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	name = strings.Trim(builder.String(), "-")
	if name == "" {
		return "artifact"
	}
	return name
}

func workspaceRoot() string {
	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}