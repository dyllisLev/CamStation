package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"camstation/internal/backup"
	"camstation/internal/camera"
	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

//go:embed web/*
var webFS embed.FS

func main() {
	var (
		addr             = flag.String("addr", getenv("CAMSTATION_ADDR", ":18080"), "HTTP listen address")
		dbPath           = flag.String("db", getenv("CAMSTATION_DB", "./data/camstation.db"), "SQLite database path")
		cameraURL        = flag.String("camera-url", getenv("CAMSTATION_CAMERA_URL", ""), "single camera URL for smoke testing")
		probeOnly        = flag.Bool("probe-only", false, "run one camera probe and exit")
		probeOnStart     = flag.Bool("probe-on-start", false, "probe CAMSTATION_CAMERA_URL during startup")
		recordingEnabled = flag.Bool("recording-enabled", getenvBool("CAMSTATION_RECORDING_ENABLED", false), "start recorder workers for registered cameras")
		recordingsDir    = flag.String("recordings-dir", getenv("CAMSTATION_RECORDINGS_DIR", "./data/recordings"), "final recording directory")
		tempDir          = flag.String("temp-dir", getenv("CAMSTATION_TEMP_DIR", "./data/temp"), "temporary recording directory")
		segmentMinutes   = flag.Int("segment-minutes", getenvInt("CAMSTATION_SEGMENT_MINUTES", 30), "recording segment length in minutes")
		maxStorageGB     = flag.Float64("max-storage-gb", getenvFloat("CAMSTATION_MAX_STORAGE_GB", 0), "maximum recording storage in GB; 0 disables automatic cleanup")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate store: %v", err)
	}
	if err := db.AppendEvent(ctx, store.Event{
		Source:  "camstationd",
		Level:   "info",
		Message: "camstationd started",
		Details: map[string]any{"addr": *addr, "db": *dbPath},
	}); err != nil {
		log.Printf("append startup event: %v", err)
	}

	prober := camera.NewFFProbe()
	streamer := stream.NewGo2RTC("./data/go2rtc.yaml")
	recorderManager := recorder.New(db, *recordingsDir, *tempDir, *segmentMinutes)
	policyCoordinator := stream.NewApplyCoordinator(db, streamer, recorderManager)
	cleaner := cleanup.New(db, *recordingsDir)
	backupRunner := backup.NewRunner(db)
	maxStorageBytes := gbToBytes(*maxStorageGB)
	recoveryResult, recoveryErr := recorder.RecoverInterruptedSegments(ctx, db, "./data/quarantine")
	recoveryLevel := "info"
	recoveryMessage := "interrupted recording recovery completed"
	recoveryDetails := map[string]any{
		"recovered":   recoveryResult.Recovered,
		"quarantined": recoveryResult.Quarantined,
		"failedMoves": recoveryResult.FailedMoves,
	}
	if recoveryErr != nil {
		recoveryLevel = "error"
		recoveryMessage = "interrupted recording recovery failed"
		recoveryDetails["error"] = recoveryErr.Error()
	}
	if recoveryResult.Recovered > 0 || recoveryErr != nil {
		_ = db.AppendEvent(ctx, store.Event{
			Source:  "recorder.recovery",
			Level:   recoveryLevel,
			Message: recoveryMessage,
			Details: recoveryDetails,
		})
	}
	runAutomaticCleanup := func() {
		limitBytes, limitErr := recordingStorageLimitBytes(context.Background(), db, maxStorageBytes)
		if limitErr != nil {
			_ = db.AppendEvent(context.Background(), store.Event{
				Source:  "recording.cleanup",
				Level:   "error",
				Message: "automatic recording cleanup failed",
				Details: map[string]any{"error": limitErr.Error()},
			})
			return
		}
		if limitBytes <= 0 {
			return
		}
		result, err := cleaner.EnforceMaxBytes(context.Background(), limitBytes)
		level := "info"
		message := "automatic recording cleanup completed"
		details := map[string]any{"maxBytes": result.MaxBytes, "beforeBytes": result.BeforeBytes, "afterBytes": result.AfterBytes, "deleted": len(result.Deleted)}
		if err != nil {
			level = "error"
			message = "automatic recording cleanup failed"
			details = map[string]any{"maxBytes": limitBytes, "error": err.Error()}
		}
		_ = db.AppendEvent(context.Background(), store.Event{
			Source:  "recording.cleanup",
			Level:   level,
			Message: message,
			Details: details,
		})
	}
	recorderManager.SetAfterSegmentClosed(runAutomaticCleanup)
	go runAutomaticCleanup()
	if err := startCameraPolicies(ctx, db, streamer, policyCoordinator, recorderManager, *recordingEnabled); err != nil {
		_ = db.AppendEvent(ctx, store.Event{
			Source:  "go2rtc",
			Level:   "error",
			Message: "go2rtc start failed",
			Details: map[string]any{"error": err.Error()},
		})
	}

	if *probeOnly {
		if *cameraURL == "" {
			log.Fatal("missing -camera-url or CAMSTATION_CAMERA_URL")
		}
		result, err := prober.Probe(ctx, *cameraURL, 12*time.Second)
		printProbe(result, err)
		if err != nil {
			os.Exit(1)
		}
		return
	}

	if *probeOnStart && *cameraURL != "" {
		go func() {
			result, err := prober.Probe(ctx, *cameraURL, 12*time.Second)
			level := "info"
			message := "camera probe succeeded"
			if err != nil {
				level = "error"
				message = "camera probe failed"
			}
			_ = db.AppendEvent(context.Background(), store.Event{
				Source:  "camera.probe",
				Level:   level,
				Message: message,
				Details: map[string]any{"result": result, "error": errString(err)},
			})
		}()
	}

	startBackupScheduler(ctx, db, backupRunner, *recordingsDir)

	mux, err := routesWithPolicyApplier(db, prober, streamer, recorderManager, cleaner, *recordingsDir, *tempDir, maxStorageBytes, *recordingEnabled, backupRunner, policyCoordinator)
	if err != nil {
		log.Fatalf("build routes: %v", err)
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		recorderManager.StopAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("camstationd listening on %s", listenURL(*addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}
