package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"camstation/internal/vieweragent"
)

var version = "dev"

func main() {
	err := run(os.Args[1:])
	if err != nil && !errors.Is(err, vieweragent.ErrAgentRestartRequested) {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Agent:", err)
	}
	os.Exit(agentExitCode(err))
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected run or configure command")
	}
	switch args[0] {
	case "configure":
		flags := flag.NewFlagSet("configure", flag.ContinueOnError)
		serverURL := flags.String("server-url", "", "CamStation server URL")
		displayName := flags.String("display-name", "", "Viewer display name")
		installDir := flags.String("install-dir", "", "machine install directory")
		configPath := flags.String("config", vieweragent.DefaultConfigPath(), "machine config path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		_, err := vieweragent.Configure(*configPath, *serverURL, *displayName, *installDir)
		return err
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		configPath := flags.String("config", "", "machine config path")
		controlStdin := flags.Bool("control-stdin", false, "accept stable host control on stdin")
		readyStdout := flags.Bool("ready-stdout", false, "signal readiness to the stable host")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *configPath == "" || !filepath.IsAbs(*configPath) {
			return fmt.Errorf("absolute --config path is required")
		}
		config, err := vieweragent.LoadConfig(*configPath)
		if err != nil {
			return err
		}
		agent := vieweragent.NewAgent(config, vieweragent.PathsFromConfig(*configPath))
		agent.AgentVersion = version
		if *readyStdout {
			agent.Ready = func() { agentReadySignal(os.Stdout) }
		}
		signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ctx, cancel := context.WithCancel(signalCtx)
		defer cancel()
		if *controlStdin {
			go func() {
				_ = watchHostControl(os.Stdin, cancel)
				cancel()
			}()
		}
		return agent.Run(ctx)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func watchHostControl(reader io.Reader, cancel context.CancelFunc) error {
	scanner := bufio.NewScanner(reader)
	if scanner.Scan() {
		if scanner.Text() != "stop" {
			return errors.New("unsupported host control command")
		}
		cancel()
		return nil
	}
	cancel()
	return scanner.Err()
}

func agentExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, vieweragent.ErrAgentRestartRequested) {
		return vieweragent.PlannedRestartExitCode
	}
	return 1
}

func agentReadySignal(writer io.Writer) { _, _ = io.WriteString(writer, "ready\n") }
