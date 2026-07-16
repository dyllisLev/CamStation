//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"camstation/internal/viewerbootstrap"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Bootstrap:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("camstation-viewer-bootstrap", flag.ContinueOnError)
	installDir := flags.String("install-dir", "", "CamStation Viewer install directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *installDir == "" || !filepath.IsAbs(*installDir) || flags.NArg() != 0 {
		return fmt.Errorf("absolute --install-dir is required")
	}
	adapter, err := viewerbootstrap.NewPlatformAdapter()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return viewerbootstrap.Run(ctx, filepath.Clean(*installDir), adapter)
}
