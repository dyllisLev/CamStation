package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"camstation/internal/legacyimport"
	"camstation/internal/store"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "operation is required: snapshot, inspect, dry-run, import, verify, or go2rtc-canary")
		return 2
	}
	operation := args[0]
	flags := flag.NewFlagSet("camstation-migrate "+operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "read-only 1.x SQLite snapshot")
	target := flags.String("target", "", "inactive 2.0 SQLite target")
	expectedCameras := flags.Int("expect-cameras", 0, "required non-archived camera count")
	expectedEnabled := flags.Int("expect-enabled", 0, "required enabled camera count")
	expectedSubStreams := flags.Int("expect-substreams", 0, "required sub-stream count")
	expectedDisabled := flags.String("expect-disabled", "", "camera key that must remain disabled")
	expectedLayouts := flags.Int("expect-layouts", 0, "required layout count")
	expectedLayoutItems := flags.Int("expect-layout-items", 0, "required total layout item count")
	expectedSegmentMinutes := flags.Int("expect-segment-minutes", 0, "required recording segment minutes")
	expectedRetentionDays := flags.Int("expect-retention-days", 0, "required recording retention days")
	expectedMaxStorageGB := flags.Float64("expect-max-storage-gb", 0, "required maximum recording storage GB")
	selectionPrefix := flags.String("select-prefix", "", "required go2rtc canary stream-key prefix")
	if err := flags.Parse(args[1:]); err != nil {
		fmt.Fprintln(stderr, "invalid command flags")
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*source) == "" {
		fmt.Fprintln(stderr, "a single operation and -source are required")
		return 2
	}
	expectations := legacyimport.Expectations{
		CameraCount:     *expectedCameras,
		EnabledCount:    *expectedEnabled,
		SubStreamCount:  *expectedSubStreams,
		DisabledCamera:  *expectedDisabled,
		LayoutCount:     *expectedLayouts,
		LayoutItemCount: *expectedLayoutItems,
		SegmentMinutes:  *expectedSegmentMinutes,
		RetentionDays:   *expectedRetentionDays,
		MaxStorageGB:    *expectedMaxStorageGB,
	}

	var manifest legacyimport.Manifest
	var err error
	switch operation {
	case "snapshot":
		if strings.TrimSpace(*target) == "" {
			fmt.Fprintln(stderr, "snapshot requires -target")
			return 2
		}
		manifest, err = legacyimport.Snapshot(ctx, *source, *target, expectations)
	case "inspect":
		manifest, err = legacyimport.Inspect(ctx, *source, expectations)
	case "dry-run":
		manifest, err = legacyimport.DryRun(ctx, *source, expectations)
	case "import":
		if strings.TrimSpace(*target) == "" {
			fmt.Fprintln(stderr, "import requires -target")
			return 2
		}
		manifest, err = legacyimport.Import(ctx, *source, *target, expectations)
	case "verify":
		if strings.TrimSpace(*target) == "" {
			fmt.Fprintln(stderr, "verify requires -target")
			return 2
		}
		manifest, err = legacyimport.Verify(ctx, *source, *target, expectations)
	case "go2rtc-canary":
		if strings.TrimSpace(*target) == "" {
			fmt.Fprintln(stderr, "go2rtc-canary requires -target")
			return 2
		}
		if strings.TrimSpace(*selectionPrefix) == "" || *expectedCameras <= 0 {
			fmt.Fprintln(stderr, "go2rtc-canary selection requires -select-prefix and positive -expect-cameras")
			return 2
		}
		manifest, err = legacyimport.ImportGo2RTCCanary(ctx, *source, *target, legacyimport.Go2RTCCanaryOptions{
			Prefix: *selectionPrefix, ExpectedCameras: *expectedCameras,
		})
	default:
		fmt.Fprintln(stderr, "unknown operation: use snapshot, inspect, dry-run, import, verify, or go2rtc-canary")
		return 2
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(manifest); encodeErr != nil {
		fmt.Fprintln(stderr, "write migration manifest failed")
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, store.RedactText(err.Error()))
		return 1
	}
	if !manifest.Ready {
		return 2
	}
	return 0
}
