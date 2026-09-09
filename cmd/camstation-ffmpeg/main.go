package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"camstation/internal/ffmpegsupervisor"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	protectParent()
	code := ffmpegsupervisor.Run(ctx, ffmpegsupervisor.Config{
		Binary: os.Getenv("CAMSTATION_FFMPEG_REAL"), StateDir: os.Getenv("CAMSTATION_NVENC_STATE_DIR"),
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	}, os.Args[1:])
	os.Exit(code)
}
