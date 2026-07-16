//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"camstation/internal/vieweragent"
	"golang.org/x/sys/windows/svc"
)

const serviceName = "CamStationViewerAgent"

type hostOptions struct {
	installDir string
	configPath string
}

func main() {
	options, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Host:", err)
		os.Exit(1)
	}
	isService, err := svc.IsWindowsService()
	if err != nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Host: service detection failed")
		os.Exit(1)
	}
	if isService {
		if err := svc.Run(serviceName, serviceHandler{options: options}); err != nil {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runHost(ctx, options); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Host: Agent stopped after bounded recovery")
		os.Exit(1)
	}
}

func parseOptions(args []string) (hostOptions, error) {
	flags := flag.NewFlagSet("camstation-viewer-host", flag.ContinueOnError)
	installDir := flags.String("install-dir", "", "machine install directory")
	configPath := flags.String("config", vieweragent.DefaultConfigPath(), "machine config path")
	if err := flags.Parse(args); err != nil {
		return hostOptions{}, err
	}
	if *installDir == "" {
		executable, err := os.Executable()
		if err != nil {
			return hostOptions{}, err
		}
		*installDir = filepath.Dir(executable)
	}
	if !filepath.IsAbs(*installDir) || !filepath.IsAbs(*configPath) {
		return hostOptions{}, fmt.Errorf("install and config paths must be absolute")
	}
	return hostOptions{installDir: filepath.Clean(*installDir), configPath: filepath.Clean(*configPath)}, nil
}

func runHost(ctx context.Context, options hostOptions) error {
	return vieweragent.RunChildSupervisor(ctx, func(childCtx context.Context) error {
		current, err := vieweragent.LoadCurrentRelease(options.installDir)
		if err != nil {
			return err
		}
		if err := vieweragent.EnsureCurrentAgentExists(current); err != nil {
			return err
		}
		command := exec.CommandContext(childCtx, current.AgentPath, "run", "--config", options.configPath)
		command.Dir = filepath.Dir(current.AgentPath)
		return command.Run()
	}, vieweragent.SleepContext)
}

type serviceHandler struct{ options hostOptions }

func (handler serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runHost(ctx, handler.options) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case <-done:
			status <- svc.Status{State: svc.StopPending}
			return false, 1
		case request, ok := <-requests:
			if !ok {
				cancel()
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				status <- current
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
				return false, 0
			}
		}
	}
}
