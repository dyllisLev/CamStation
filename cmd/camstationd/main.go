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
	"camstation/internal/opslog"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

//go:embed web/*
var webFS embed.FS

func main() {
	var (
		addr              = flag.String("addr", getenv("CAMSTATION_ADDR", ":18080"), "HTTP listen address")
		dbPath            = flag.String("db", getenv("CAMSTATION_DB", "./data/camstation.db"), "SQLite database path")
		cameraURL         = flag.String("camera-url", getenv("CAMSTATION_CAMERA_URL", ""), "single camera URL for smoke testing")
		probeOnly         = flag.Bool("probe-only", false, "run one camera probe and exit")
		probeOnStart      = flag.Bool("probe-on-start", false, "probe CAMSTATION_CAMERA_URL during startup")
		recordingEnabled  = flag.Bool("recording-enabled", getenvBool("CAMSTATION_RECORDING_ENABLED", false), "start recorder workers for registered cameras")
		recordingsDir     = flag.String("recordings-dir", getenv("CAMSTATION_RECORDINGS_DIR", "./data/recordings"), "final recording directory")
		tempDir           = flag.String("temp-dir", getenv("CAMSTATION_TEMP_DIR", "./data/temp"), "temporary recording directory")
		viewerReleasesDir = flag.String("viewer-releases-dir", getenv("CAMSTATION_VIEWER_RELEASES_DIR", "./data/viewer-releases"), "Windows Viewer release directory")
		webrtcCandidates  = flag.String("webrtc-candidates", getenv("CAMSTATION_WEBRTC_CANDIDATES", ""), "comma-separated external WebRTC IP:port candidates")
		segmentMinutes    = flag.Int("segment-minutes", getenvInt("CAMSTATION_SEGMENT_MINUTES", 30), "recording segment length in minutes")
		maxStorageGB      = flag.Float64("max-storage-gb", getenvFloat("CAMSTATION_MAX_STORAGE_GB", 0), "maximum recording storage in GB; 0 disables automatic cleanup")
	)
	flag.Parse()
	operationalLogger, err := opslog.NewFromEnvironment(os.Stdout, *dbPath)
	if err != nil {
		log.Fatalf("configure operational logging: %v", err)
	}
	defer operationalLogger.Close()
	log.SetFlags(0)
	log.SetOutput(opslog.NewLineWriter(operationalLogger, "daemon.legacy", opslog.Info))
	_ = operationalLogger.Log(opslog.Info, "daemon", "startup_started", opslog.Fields{State: "starting"})

	parsedWebRTCCandidates, err := stream.ParseWebRTCCandidates(*webrtcCandidates)
	if err != nil {
		_ = operationalLogger.Log(opslog.Error, "daemon", "startup_failed", opslog.Fields{
			Phase: "webrtc_config", ErrorCode: "invalid_webrtc_candidates", Message: err.Error(),
		})
		log.Fatalf("parse WebRTC candidates: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(*dbPath)
	if err != nil {
		_ = operationalLogger.Log(opslog.Error, "daemon", "startup_failed", opslog.Fields{
			Phase: "store_open", ErrorCode: "store_open_failed", Message: err.Error(),
		})
		log.Fatalf("open store: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		_ = operationalLogger.Log(opslog.Error, "daemon", "startup_failed", opslog.Fields{
			Phase: "store_migrate", ErrorCode: "store_migrate_failed", Message: err.Error(),
		})
		log.Fatalf("migrate store: %v", err)
	}
	if err := db.AppendEvent(ctx, store.Event{
		Source:  "camstationd",
		Level:   "info",
		Message: "camstationd started",
		Details: map[string]any{"state": "running"},
	}); err != nil {
		_ = operationalLogger.Log(opslog.Warn, "daemon", "startup_event_failed", opslog.Fields{
			ErrorCode: "event_append_failed", Message: err.Error(),
		})
		log.Printf("append startup event: %v", err)
	}

	prober := camera.NewFFProbe()
	streamer := stream.NewGo2RTC("./data/go2rtc.yaml", stream.WithWebRTCCandidates(parsedWebRTCCandidates), stream.WithLogger(operationalLogger))
	recorderManager := recorder.New(db, *recordingsDir, *tempDir, *segmentMinutes, recorder.WithLogger(operationalLogger))
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
	streamReady := true
	if err := startCameraPolicies(ctx, db, streamer, policyCoordinator, recorderManager, *recordingEnabled); err != nil {
		streamReady = false
		_ = db.AppendEvent(ctx, store.Event{
			Source:  "go2rtc",
			Level:   "error",
			Message: "go2rtc start failed",
			Details: map[string]any{"error": err.Error()},
		})
	}
	if streamReady {
		if err := startLiveWarmReconciler(ctx, db, streamer, 2*time.Second, func(err error) {
			_ = db.AppendEvent(context.Background(), store.Event{
				Source:  "stream.warm",
				Level:   "warning",
				Message: "live warm consumer reconciliation failed",
				Details: map[string]any{"error": store.RedactText(err.Error())},
			})
		}); err != nil {
			_ = db.AppendEvent(ctx, store.Event{
				Source:  "stream.warm",
				Level:   "error",
				Message: "live warm consumers failed to start",
				Details: map[string]any{"error": store.RedactText(err.Error())},
			})
		}
	}

	if *probeOnly {
		if *cameraURL == "" {
			_ = operationalLogger.Log(opslog.Error, "daemon", "probe_failed", opslog.Fields{
				ErrorCode: "camera_url_missing",
			})
			log.Fatal("missing -camera-url or CAMSTATION_CAMERA_URL")
		}
		result, err := prober.Probe(ctx, *cameraURL, 12*time.Second)
		printProbe(result, err)
		if err != nil {
			_ = operationalLogger.Log(opslog.Error, "camera.probe", "probe_failed", opslog.Fields{
				ErrorCode: "probe_failed", Message: err.Error(),
			})
			os.Exit(1)
		}
		_ = operationalLogger.Log(opslog.Info, "camera.probe", "probe_succeeded", opslog.Fields{State: "ready"})
		return
	}

	if *probeOnStart && *cameraURL != "" {
		go func() {
			result, err := prober.Probe(ctx, *cameraURL, 12*time.Second)
			level := "info"
			message := "camera probe succeeded"
			operationalLevel := opslog.Info
			operationalEvent := "probe_succeeded"
			if err != nil {
				level = "error"
				message = "camera probe failed"
				operationalLevel = opslog.Error
				operationalEvent = "probe_failed"
			}
			operationalFields := opslog.Fields{State: "ready"}
			if err != nil {
				operationalFields.State = "failed"
				operationalFields.ErrorCode = "probe_failed"
				operationalFields.Message = err.Error()
			}
			_ = operationalLogger.Log(operationalLevel, "camera.probe", operationalEvent, operationalFields)
			_ = db.AppendEvent(context.Background(), store.Event{
				Source:  "camera.probe",
				Level:   level,
				Message: message,
				Details: map[string]any{"result": result, "error": errString(err)},
			})
		}()
	}

	startBackupScheduler(ctx, db, backupRunner, *recordingsDir)

	mux, err := routesWithPolicyApplierAndLogger(db, prober, streamer, recorderManager, cleaner, *recordingsDir, *tempDir,
		maxStorageBytes, *recordingEnabled, backupRunner, policyCoordinator, operationalLogger, *viewerReleasesDir)
	if err != nil {
		_ = operationalLogger.Log(opslog.Error, "daemon", "startup_failed", opslog.Fields{
			Phase: "routes", ErrorCode: "route_build_failed", Message: err.Error(),
		})
		log.Fatalf("build routes: %v", err)
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		_ = operationalLogger.Log(opslog.Info, "daemon", "shutdown_started", opslog.Fields{State: "stopping"})
		recorderManager.StopAll()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	_ = operationalLogger.Log(opslog.Info, "daemon", "listening", opslog.Fields{State: "ready"})
	log.Printf("camstationd listening on %s", listenURL(*addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		_ = operationalLogger.Log(opslog.Error, "daemon", "server_failed", opslog.Fields{
			ErrorCode: "listen_failed", Message: err.Error(),
		})
		log.Fatalf("serve: %v", err)
	}
	_ = operationalLogger.Log(opslog.Info, "daemon", "stopped", opslog.Fields{State: "stopped"})
}
