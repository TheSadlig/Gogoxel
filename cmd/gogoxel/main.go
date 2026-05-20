package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"strings"

	"Gogoxel/internal/automation/grpcserver"
	"Gogoxel/internal/game"

	"google.golang.org/grpc"
)

func main() {
	automationMode := flag.Bool("automation", false, "run the engine in automation mode")
	listenAddress := flag.String("listen", "127.0.0.1:50051", "automation gRPC listen address")
	liveListenAddress := flag.String("automation-listen", "", "serve automation gRPC while running the engine session")
	headless := flag.Bool("headless", false, "run automation without initializing GLFW or Vulkan")
	hiddenWindow := flag.Bool("hidden-window", false, "hide the window when the session initializes the renderer")
	tickRateHz := flag.Int("tick-rate", 60, "fixed automation simulation tick rate")
	artifactDir := flag.String("artifact-dir", "artifacts/automation", "directory for automation artifacts")
	flag.Parse()

	if *automationMode {
		if err := runSession(*listenAddress, game.HostOptions{
			Headless:     *headless,
			HiddenWindow: *hiddenWindow,
			TickRateHz:   *tickRateHz,
			ArtifactDir:  *artifactDir,
		}); err != nil {
			log.Fatal(err)
		}
		return
	}

	if strings.TrimSpace(*liveListenAddress) == "" && (*headless || *hiddenWindow) {
		log.Fatal("headless and hidden-window session modes require --automation or --automation-listen")
	}

	if err := runSession(*liveListenAddress, game.HostOptions{
		Headless:     *headless,
		HiddenWindow: *hiddenWindow,
		Live:         true,
		TickRateHz:   *tickRateHz,
		ArtifactDir:  *artifactDir,
	}); err != nil {
		log.Fatal(err)
	}
}

func runSession(listenAddress string, options game.HostOptions) error {
	host := game.NewHost(options)
	var (
		listener   net.Listener
		server     *grpc.Server
		serverDone chan error
	)
	if strings.TrimSpace(listenAddress) != "" {
		var err error
		listener, err = net.Listen("tcp", listenAddress)
		if err != nil {
			return err
		}
		defer listener.Close()

		server = grpc.NewServer()
		grpcserver.Register(server, host)
		serverDone = make(chan error, 1)
		go func() {
			serverDone <- server.Serve(listener)
		}()

		log.Printf("automation gRPC listening on %s", listener.Addr())
	}
	runErr := host.Run(context.Background())
	if server != nil {
		server.GracefulStop()
		serveErr := <-serverDone
		if serveErr != nil && !errors.Is(serveErr, grpc.ErrServerStopped) {
			return serveErr
		}
	}
	return runErr
}
