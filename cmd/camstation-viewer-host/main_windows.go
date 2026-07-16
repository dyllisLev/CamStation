//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"camstation/internal/vieweragent"
	"golang.org/x/sys/windows/svc"
)

const (
	serviceName              = "CamStationViewerAgent"
	agentGracefulStopTimeout = 10 * time.Second
	hostShutdownTimeout      = 2*agentGracefulStopTimeout + 2*time.Second
)

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
	initial, err := prepareHost(options, func() {})
	if err != nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Host: Agent is not ready")
		os.Exit(1)
	}
	if err := runHost(ctx, options, initial); err != nil && ctx.Err() == nil {
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

func runHost(ctx context.Context, options hostOptions, initial vieweragent.HostChild) error {
	next := initial
	return vieweragent.RunChildSupervisor(ctx, func(childCtx context.Context) vieweragent.ChildExit {
		child := next
		next = nil
		if child == nil {
			current, err := vieweragent.LoadReadyRelease(options.installDir)
			if err != nil {
				return vieweragent.ChildExit{Kind: vieweragent.ChildCrashed, Err: err}
			}
			child, err = startAgentChild(current, options.configPath)
			if err != nil {
				return vieweragent.ChildExit{Kind: vieweragent.ChildCrashed, Err: err}
			}
		}
		return vieweragent.RunManagedChild(childCtx, child, agentGracefulStopTimeout)
	}, vieweragent.SleepContext)
}

func prepareHost(options hostOptions, ready func()) (vieweragent.HostChild, error) {
	return vieweragent.EstablishHostReadiness(func() (vieweragent.CurrentRelease, error) {
		return vieweragent.LoadReadyRelease(options.installDir)
	}, func(current vieweragent.CurrentRelease) (vieweragent.HostChild, error) {
		return startAgentChild(current, options.configPath)
	}, ready)
}

type agentChild struct {
	command *exec.Cmd
	stdin   io.WriteCloser
}

func startAgentChild(current vieweragent.CurrentRelease, configPath string) (vieweragent.HostChild, error) {
	command := exec.Command(current.AgentPath, "run", "--config", configPath, "--control-stdin", "--ready-stdout")
	command.Dir = filepath.Dir(current.AgentPath)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := vieweragent.WaitForAgentReady(stdout, 15*time.Second); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	return &agentChild{command: command, stdin: stdin}, nil
}

func (child *agentChild) Wait() error { return child.command.Wait() }

func (child *agentChild) RequestStop() error {
	_, err := io.WriteString(child.stdin, "stop\n")
	_ = child.stdin.Close()
	return err
}

func (child *agentChild) Kill() error { return child.command.Process.Kill() }

type serviceHandler struct{ options hostOptions }

func (handler serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	initial, err := prepareHost(handler.options, func() {})
	if err != nil {
		status <- svc.Status{State: svc.StopPending}
		return false, 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runHost(ctx, handler.options, initial) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case <-done:
			status <- svc.Status{State: svc.StopPending}
			return false, 0
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
				case <-time.After(hostShutdownTimeout):
				}
				return false, 0
			}
		}
	}
}
