package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"camstation/internal/vieweragent"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Agent:", err)
		os.Exit(1)
	}
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
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return agent.Run(ctx)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
