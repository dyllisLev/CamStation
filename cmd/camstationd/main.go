package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"camstation/internal/camera"
	"camstation/internal/cameraprofile"
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
	cleaner := cleanup.New(db, *recordingsDir)
	maxStorageBytes := int64(*maxStorageGB * 1024 * 1024 * 1024)
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
	if maxStorageBytes > 0 {
		runAutomaticCleanup := func() {
			result, err := cleaner.EnforceMaxBytes(context.Background(), maxStorageBytes)
			level := "info"
			message := "automatic recording cleanup completed"
			details := map[string]any{"maxBytes": result.MaxBytes, "beforeBytes": result.BeforeBytes, "afterBytes": result.AfterBytes, "deleted": len(result.Deleted)}
			if err != nil {
				level = "error"
				message = "automatic recording cleanup failed"
				details = map[string]any{"maxBytes": maxStorageBytes, "error": err.Error()}
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
	}
	if cameras, err := db.ListCameras(ctx, true); err == nil && len(cameras) > 0 {
		if err := streamer.Ensure(ctx, cameras); err != nil {
			_ = db.AppendEvent(ctx, store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc start failed",
				Details: map[string]any{"error": err.Error()},
			})
		}
		if *recordingEnabled {
			recorderManager.Reconcile(cameras)
			_ = db.AppendEvent(ctx, store.Event{
				Source:  "recorder",
				Level:   "info",
				Message: "recorder workers started",
				Details: map[string]any{"cameras": len(cameras), "input": "go2rtc-local-rtsp"},
			})
		}
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

	mux, err := routes(db, prober, streamer, recorderManager, cleaner, *recordingsDir, *tempDir, maxStorageBytes, *recordingEnabled)
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

func routes(db *store.DB, prober camera.Prober, streamer *stream.Go2RTC, recorderManager *recorder.Manager, cleaner *cleanup.Cleaner, recordingsDir, tempDir string, maxStorageBytes int64, recordingEnabled bool) (http.Handler, error) {
	mux := http.NewServeMux()
	previews := newPreviewRegistry()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"mode":      "development",
			"startedAt": time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := db.ListEvents(r.Context(), 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	})

	mux.HandleFunc("GET /api/cameras", func(w http.ResponseWriter, r *http.Request) {
		cameras, err := db.ListCameras(r.Context(), false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, cameras)
	})

	mux.HandleFunc("GET /api/layouts", func(w http.ResponseWriter, r *http.Request) {
		layouts, err := db.ListLayouts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, layouts)
	})

	mux.HandleFunc("POST /api/layouts", func(w http.ResponseWriter, r *http.Request) {
		var req store.LayoutProfile
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		req.ID = layoutID()
		layout, err := db.CreateLayout(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, layout)
	})

	mux.HandleFunc("PUT /api/layouts/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req store.LayoutProfile
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		layout, err := db.UpdateLayout(r.Context(), r.PathValue("id"), req)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, layout)
	})

	mux.HandleFunc("GET /api/timeline", func(w http.ResponseWriter, r *http.Request) {
		streamName := r.URL.Query().Get("cam")
		date := r.URL.Query().Get("date")
		if streamName == "" || date == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("cam and date are required"))
			return
		}
		from, to, err := dayRangeKST(date)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		segmentStreamName := streamName
		if cameras, err := db.ListCameras(r.Context(), true); err == nil {
			if camera, ok := cameraByStream(cameras, streamName); ok && camera.RecordingStreamName != "" {
				segmentStreamName = camera.RecordingStreamName
			}
		}
		segments, err := db.ListRecordingSegments(r.Context(), segmentStreamName, from, to, "ready", "recording")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"segments":      timelineSegments(segments),
			"motion_events": []any{},
		})
	})

	mux.HandleFunc("GET /api/recorders/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, recorderManager.Status())
	})

	mux.HandleFunc("POST /api/recorders/start", func(w http.ResponseWriter, r *http.Request) {
		cameras, err := db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		streamName := r.URL.Query().Get("stream")
		if streamName != "" {
			camera, ok := cameraByStream(cameras, streamName)
			if !ok {
				writeError(w, http.StatusNotFound, fmt.Errorf("camera stream not found: %s", streamName))
				return
			}
			if err := recorderManager.Start(camera); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			recorderManager.Reconcile(cameras)
		}
		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "recorder",
			Level:   "info",
			Message: "recorder workers started",
			Details: map[string]any{"stream": streamName, "cameras": len(cameras), "input": "go2rtc-local-rtsp"},
		})
		writeJSON(w, http.StatusOK, recorderManager.Status())
	})

	mux.HandleFunc("POST /api/recorders/stop", func(w http.ResponseWriter, r *http.Request) {
		streamName := r.URL.Query().Get("stream")
		if streamName != "" {
			recorderManager.Stop(streamName)
		} else {
			recorderManager.StopAll()
		}
		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "recorder",
			Level:   "info",
			Message: "recorder workers stopped",
			Details: map[string]any{"stream": streamName},
		})
		writeJSON(w, http.StatusOK, recorderManager.Status())
	})

	mux.HandleFunc("GET /api/recordings/storage", func(w http.ResponseWriter, r *http.Request) {
		recordingsBytes, err := cleanup.DirSize(recordingsDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		tempBytes, err := cleanup.DirSize(tempDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"recordingsDir":      recordingsDir,
			"tempDir":            tempDir,
			"recordingsBytes":    recordingsBytes,
			"tempBytes":          tempBytes,
			"maxBytes":           maxStorageBytes,
			"autoCleanupEnabled": maxStorageBytes > 0,
		})
	})

	mux.HandleFunc("POST /api/recordings/cleanup", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxBytes     int64   `json:"maxBytes"`
			MaxStorageGB float64 `json:"maxStorageGB"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		if req.MaxBytes <= 0 && req.MaxStorageGB > 0 {
			req.MaxBytes = int64(req.MaxStorageGB * 1024 * 1024 * 1024)
		}
		if req.MaxBytes <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("maxBytes or maxStorageGB is required"))
			return
		}
		result, err := cleaner.EnforceMaxBytes(r.Context(), req.MaxBytes)
		if err != nil {
			_ = db.AppendEvent(r.Context(), store.Event{
				Source:  "recording.cleanup",
				Level:   "error",
				Message: "recording cleanup failed",
				Details: map[string]any{"error": err.Error(), "maxBytes": req.MaxBytes},
			})
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "recording.cleanup",
			Level:   "info",
			Message: "recording cleanup completed",
			Details: map[string]any{"maxBytes": result.MaxBytes, "beforeBytes": result.BeforeBytes, "afterBytes": result.AfterBytes, "deleted": len(result.Deleted)},
		})
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/cameras/scan", func(w http.ResponseWriter, r *http.Request) {
		var req cameraprofile.ScanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		profile, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profile": redactDeviceProfile(profile)})
	})

	mux.HandleFunc("POST /api/cameras/preview", func(w http.ResponseWriter, r *http.Request) {
		var req cameraPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		profile, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(r.Context(), req.ScanRequest)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		candidates := selectProfileCandidates(profile, req.ChannelIndexValue(), []cameraStreamSelection{{
			Role:         req.Role,
			ProfileToken: req.ProfileToken,
		}})
		if len(candidates) == 0 || candidates[0].URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("preview profile candidate not found"))
			return
		}
		streamName, expiresAt := previews.Put(candidates[0].URL, 10*time.Minute)
		writeJSON(w, http.StatusOK, map[string]any{
			"streamName": streamName,
			"expiresAt":  expiresAt.UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /api/cameras/{streamName}/scan", func(w http.ResponseWriter, r *http.Request) {
		existing, err := db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		var req cameraCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		req = cameraUpdateRequest(existing, req)
		profile, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(r.Context(), scanRequestFromCamera(req))
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "profile": redactDeviceProfile(profile)})
	})

	mux.HandleFunc("POST /api/cameras/{streamName}/preview", func(w http.ResponseWriter, r *http.Request) {
		existing, err := db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		var req cameraPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		req = previewRequestWithExisting(existing, req)
		profile, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(r.Context(), req.ScanRequest)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		candidates := selectProfileCandidates(profile, req.ChannelIndexValue(), []cameraStreamSelection{{
			Role:         req.Role,
			ProfileToken: req.ProfileToken,
		}})
		if len(candidates) == 0 || candidates[0].URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("preview profile candidate not found"))
			return
		}
		streamName, expiresAt := previews.Put(candidates[0].URL, 10*time.Minute)
		writeJSON(w, http.StatusOK, map[string]any{
			"streamName": streamName,
			"expiresAt":  expiresAt.UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /api/cameras", func(w http.ResponseWriter, r *http.Request) {
		var req cameraCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		saved, result, probeErr, err := persistCameraProfile(r.Context(), db, prober, req, "")
		if err != nil {
			if errors.Is(err, errBadCameraProfileRequest) {
				writeError(w, http.StatusBadRequest, err)
			} else if errors.Is(err, errCameraProfileScanFailed) {
				writeError(w, http.StatusBadGateway, err)
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		level, message := cameraMutationEvent("camera registered", probeErr)
		publicSaved := saved
		sanitizeCameraSecrets(&publicSaved)

		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   level,
			Message: message,
			Details: map[string]any{
				"name":    saved.Name,
				"stream":  saved.StreamName,
				"state":   saved.State,
				"adapter": saved.ProfileAdapter,
				"result":  result,
				"error":   errString(probeErr),
			},
		})

		cameras, err := db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := streamer.Restart(r.Context(), cameras); err != nil {
			_ = db.AppendEvent(r.Context(), store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc restart failed",
				Details: map[string]any{"error": err.Error()},
			})
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      probeErr == nil,
				"camera":  publicSaved,
				"go2rtc":  streamer.Status(r.Context()),
				"warning": err.Error(),
			})
			return
		}
		if recordingEnabled {
			recorderManager.Reconcile(cameras)
		}

		status := http.StatusOK
		if probeErr != nil {
			status = http.StatusAccepted
		}
		writeJSON(w, status, map[string]any{"ok": probeErr == nil, "camera": publicSaved, "go2rtc": streamer.Status(r.Context())})
	})

	mux.HandleFunc("PUT /api/cameras/{streamName}", func(w http.ResponseWriter, r *http.Request) {
		existing, err := db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		var req cameraCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		req = cameraUpdateRequest(existing, req)
		saved, result, probeErr, err := persistCameraProfile(r.Context(), db, prober, req, existing.StreamName)
		if err != nil {
			if errors.Is(err, errBadCameraProfileRequest) {
				writeError(w, http.StatusBadRequest, err)
			} else if errors.Is(err, errCameraProfileScanFailed) {
				writeError(w, http.StatusBadGateway, err)
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		level, message := cameraMutationEvent("camera updated", probeErr)
		publicSaved := saved
		sanitizeCameraSecrets(&publicSaved)

		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   level,
			Message: message,
			Details: map[string]any{
				"name":    saved.Name,
				"stream":  saved.StreamName,
				"state":   saved.State,
				"adapter": saved.ProfileAdapter,
				"result":  result,
				"error":   errString(probeErr),
			},
		})

		cameras, err := db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := streamer.Restart(r.Context(), cameras); err != nil {
			_ = db.AppendEvent(r.Context(), store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc restart failed",
				Details: map[string]any{"error": err.Error()},
			})
			if recordingEnabled {
				recorderManager.Reconcile(cameras)
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      probeErr == nil,
				"camera":  publicSaved,
				"go2rtc":  streamer.Status(r.Context()),
				"warning": err.Error(),
			})
			return
		}
		if recordingEnabled {
			recorderManager.Reconcile(cameras)
		}

		status := http.StatusOK
		if probeErr != nil {
			status = http.StatusAccepted
		}
		writeJSON(w, status, map[string]any{"ok": probeErr == nil, "camera": publicSaved, "go2rtc": streamer.Status(r.Context())})
	})

	mux.HandleFunc("DELETE /api/cameras/{streamName}", func(w http.ResponseWriter, r *http.Request) {
		deleted, err := db.DeleteCamera(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		publicDeleted := deleted
		sanitizeCameraSecrets(&publicDeleted)

		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   "warning",
			Message: "camera deleted",
			Details: map[string]any{
				"name":   deleted.Name,
				"stream": deleted.StreamName,
				"roles":  len(deleted.Streams),
			},
		})

		cameras, err := db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := streamer.Restart(r.Context(), cameras); err != nil {
			_ = db.AppendEvent(r.Context(), store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc restart failed",
				Details: map[string]any{"error": err.Error()},
			})
			if recordingEnabled {
				recorderManager.Reconcile(cameras)
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      true,
				"camera":  publicDeleted,
				"go2rtc":  streamer.Status(r.Context()),
				"warning": err.Error(),
			})
			return
		}
		if recordingEnabled {
			recorderManager.Reconcile(cameras)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "camera": publicDeleted, "go2rtc": streamer.Status(r.Context())})
	})

	mux.HandleFunc("GET /api/streams/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, streamer.Status(r.Context()))
	})

	mux.HandleFunc("POST /api/streams/restart", func(w http.ResponseWriter, r *http.Request) {
		cameras, err := db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := streamer.Restart(r.Context(), cameras); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if recordingEnabled {
			recorderManager.Reconcile(cameras)
		}
		writeJSON(w, http.StatusOK, streamer.Status(r.Context()))
	})

	liveProxy, err := go2RTCProxy(previews)
	if err != nil {
		return nil, err
	}
	mux.Handle("/player/", http.StripPrefix("/player", liveProxy))

	mux.HandleFunc("POST /api/camera/probe", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL     string `json:"url"`
			Timeout int    `json:"timeoutSeconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("url is required"))
			return
		}
		timeout := time.Duration(req.Timeout) * time.Second
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 12 * time.Second
		}

		result, err := prober.Probe(r.Context(), req.URL, timeout)
		level := "info"
		message := "camera probe succeeded"
		status := http.StatusOK
		if err != nil {
			level = "error"
			message = "camera probe failed"
			status = http.StatusBadGateway
		}
		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "camera.probe",
			Level:   level,
			Message: message,
			Details: map[string]any{"result": result, "error": errString(err)},
		})
		if err != nil {
			writeJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "result": result})
			return
		}
		writeJSON(w, status, map[string]any{"ok": true, "result": result})
	})

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	mux.Handle("/", spaHandler(http.FS(static)))
	return mux, nil
}

func layoutID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func dayRangeKST(date string) (time.Time, time.Time, error) {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		location = time.FixedZone("KST", 9*60*60)
	}
	start, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date format; expected YYYY-MM-DD")
	}
	return start, start.Add(24 * time.Hour), nil
}

func timelineSegments(segments []store.RecordingSegment) []map[string]any {
	out := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		out = append(out, map[string]any{
			"camera_id": segment.StreamName,
			"filename":  segment.Filename,
			"ts_start":  segment.TSStart,
			"ts_end":    segment.TSEnd,
			"file_size": segment.FileSize,
			"status":    segment.Status,
		})
	}
	return out
}

type cameraStreamSelection struct {
	Role         cameraprofile.StreamRole `json:"role"`
	ProfileToken string                   `json:"profileToken"`
}

type cameraCreateRequest struct {
	Name             string                          `json:"name"`
	URL              string                          `json:"url"`
	Stream           string                          `json:"streamName"`
	Host             string                          `json:"host"`
	Username         string                          `json:"username"`
	Password         string                          `json:"password"`
	RTSPPort         int                             `json:"rtspPort"`
	HTTPPort         int                             `json:"httpPort"`
	ONVIFPort        int                             `json:"onvifPort"`
	Adapter          string                          `json:"adapter"`
	ChannelIndex     *int                            `json:"channelIndex"`
	Profile          cameraprofile.DeviceProfile     `json:"profile"`
	Streams          []cameraprofile.StreamCandidate `json:"streams"`
	StreamSelections []cameraStreamSelection         `json:"streamSelections"`
}

var (
	errBadCameraProfileRequest = errors.New("bad camera profile request")
	errCameraProfileScanFailed = errors.New("camera profile scan failed")
)

func (r cameraCreateRequest) ChannelIndexValue() int {
	if r.ChannelIndex == nil {
		return 0
	}
	return *r.ChannelIndex
}

func cameraUpdateRequest(existing store.Camera, req cameraCreateRequest) cameraCreateRequest {
	req.Stream = existing.StreamName
	if req.Name == "" {
		req.Name = existing.Name
	}
	if req.URL == "" {
		req.URL = existing.URL
	}
	if req.Host == "" {
		req.Host = existing.Host
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = existing.RTSPPort
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = existing.HTTPPort
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = existing.ONVIFPort
	}
	if req.Adapter == "" {
		req.Adapter = existing.ProfileAdapter
	}
	if req.ChannelIndex == nil {
		req.ChannelIndex = existing.ChannelIndex
	}
	return req
}

func scanRequestFromCamera(req cameraCreateRequest) cameraprofile.ScanRequest {
	return cameraprofile.ScanRequest{
		Name:      req.Name,
		URL:       req.URL,
		Host:      req.Host,
		Username:  req.Username,
		Password:  req.Password,
		RTSPPort:  req.RTSPPort,
		HTTPPort:  req.HTTPPort,
		ONVIFPort: req.ONVIFPort,
		Adapter:   req.Adapter,
	}
}

func persistCameraProfile(ctx context.Context, db *store.DB, prober camera.Prober, req cameraCreateRequest, stableStreamName string) (store.Camera, camera.ProbeResult, error, error) {
	if req.Name == "" {
		req.Name = "Camera 1"
	}
	if stableStreamName != "" {
		req.Stream = stableStreamName
	}
	if req.Stream == "" {
		req.Stream = streamName(req.Name, 1)
	}

	profile := req.Profile
	scanReq := scanRequestFromCamera(req)
	if !hasProfileCandidates(profile) && scanReqHasTarget(scanReq) {
		scanned, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(ctx, scanReq)
		if err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: %v", errCameraProfileScanFailed, err)
		}
		profile = scanned
	}

	candidates := profileCandidates(profile)
	if len(req.Streams) > 0 {
		candidates = req.Streams
	}
	if len(req.StreamSelections) > 0 {
		candidates = selectProfileCandidates(profile, req.ChannelIndexValue(), req.StreamSelections)
		if len(candidates) == 0 {
			return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: selected stream profiles were not found", errBadCameraProfileRequest)
		}
	}
	primaryURL := req.URL
	if primaryURL == "" {
		primaryURL = primaryCandidateURL(candidates)
	}
	if primaryURL == "" {
		return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: url or stream candidates are required", errBadCameraProfileRequest)
	}
	if prober == nil {
		return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("camera prober unavailable")
	}

	result, probeErr := prober.Probe(ctx, primaryURL, 12*time.Second)
	state := "streaming"
	if probeErr != nil {
		state = "offline"
	}

	saved, err := db.UpsertCamera(ctx, store.Camera{
		Name:           req.Name,
		URL:            primaryURL,
		StreamName:     req.Stream,
		State:          state,
		Manufacturer:   profile.Manufacturer,
		Model:          profile.Model,
		ProfileAdapter: profile.Adapter,
		Host:           firstNonEmpty(profile.Host, scanReq.Host),
		RTSPPort:       firstNonZero(profile.RTSPPort, scanReq.RTSPPort),
		HTTPPort:       firstNonZero(profile.HTTPPort, scanReq.HTTPPort),
		ONVIFPort:      firstNonZero(profile.ONVIFPort, scanReq.ONVIFPort),
		ChannelIndex:   req.ChannelIndex,
		LastProbeJSON:  toMap(result),
		LastScanJSON:   profile.LastScan,
	})
	if err != nil {
		return store.Camera{}, camera.ProbeResult{}, nil, err
	}
	if len(candidates) > 0 {
		if err := db.ReplaceCameraStreams(ctx, saved.ID, toStoreStreams(saved.StreamName, candidates, state)); err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, err
		}
		saved, err = db.GetCameraByStream(ctx, saved.StreamName)
		if err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, err
		}
	}
	return saved, result, probeErr, nil
}

func cameraMutationEvent(successMessage string, probeErr error) (string, string) {
	if probeErr != nil {
		return "error", successMessage + " but probe failed"
	}
	return "info", successMessage
}

type cameraPreviewRequest struct {
	cameraprofile.ScanRequest
	ChannelIndex *int                     `json:"channelIndex"`
	ProfileToken string                   `json:"profileToken"`
	Role         cameraprofile.StreamRole `json:"role"`
}

func (r cameraPreviewRequest) ChannelIndexValue() int {
	if r.ChannelIndex == nil {
		return 0
	}
	return *r.ChannelIndex
}

func previewRequestWithExisting(existing store.Camera, req cameraPreviewRequest) cameraPreviewRequest {
	if req.Name == "" {
		req.Name = existing.Name
	}
	if req.URL == "" {
		req.URL = existing.URL
	}
	if req.Host == "" {
		req.Host = existing.Host
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = existing.RTSPPort
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = existing.HTTPPort
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = existing.ONVIFPort
	}
	if req.Adapter == "" {
		req.Adapter = existing.ProfileAdapter
	}
	if req.ChannelIndex == nil {
		req.ChannelIndex = existing.ChannelIndex
	}
	return req
}

func scanReqHasTarget(req cameraprofile.ScanRequest) bool {
	return req.Host != "" || req.URL != ""
}

func hasProfileCandidates(profile cameraprofile.DeviceProfile) bool {
	return len(profileCandidates(profile)) > 0
}

func profileCandidates(profile cameraprofile.DeviceProfile) []cameraprofile.StreamCandidate {
	var candidates []cameraprofile.StreamCandidate
	for _, channel := range profile.Channels {
		candidates = append(candidates, channel.Candidates...)
	}
	return candidates
}

func primaryCandidateURL(candidates []cameraprofile.StreamCandidate) string {
	for _, candidate := range candidates {
		if candidate.RoleHint == cameraprofile.StreamRoleRecording && candidate.URL != "" {
			return candidate.URL
		}
	}
	for _, candidate := range candidates {
		if candidate.URL != "" {
			return candidate.URL
		}
	}
	return ""
}

func selectProfileCandidates(profile cameraprofile.DeviceProfile, channelIndex int, selections []cameraStreamSelection) []cameraprofile.StreamCandidate {
	channel := profileChannel(profile, channelIndex)
	if channel == nil {
		return nil
	}
	selected := make([]cameraprofile.StreamCandidate, 0, len(selections))
	for _, selection := range selections {
		if selection.ProfileToken == "" {
			continue
		}
		candidate, ok := candidateByProfileToken(channel.Candidates, selection.ProfileToken)
		if !ok {
			continue
		}
		if selection.Role != "" {
			candidate.RoleHint = selection.Role
		}
		selected = append(selected, candidate)
	}
	return selected
}

func profileChannel(profile cameraprofile.DeviceProfile, channelIndex int) *cameraprofile.ChannelProfile {
	for i := range profile.Channels {
		if profile.Channels[i].Index == channelIndex {
			return &profile.Channels[i]
		}
	}
	if len(profile.Channels) == 0 {
		return nil
	}
	return &profile.Channels[0]
}

func candidateByProfileToken(candidates []cameraprofile.StreamCandidate, token string) (cameraprofile.StreamCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.ProfileToken == token {
			return candidate, true
		}
	}
	return cameraprofile.StreamCandidate{}, false
}

func toStoreStreams(base string, candidates []cameraprofile.StreamCandidate, state string) []store.CameraStream {
	streams := make([]store.CameraStream, 0, len(candidates))
	used := map[string]int{}
	for _, candidate := range candidates {
		if candidate.URL == "" {
			continue
		}
		role := store.CameraStreamRole(candidate.RoleHint)
		if role == "" {
			role = store.CameraStreamRoleRecording
		}
		name := roleStreamName(base, role)
		if used[name] > 0 {
			used[name]++
			name = fmt.Sprintf("%s-%d", name, used[name])
		} else {
			used[name] = 1
		}
		streams = append(streams, store.CameraStream{
			Role:             role,
			Label:            candidate.Label,
			Source:           candidate.Source,
			URL:              candidate.URL,
			Go2RTCStreamName: name,
			Codec:            candidate.Codec,
			Width:            candidate.Width,
			Height:           candidate.Height,
			FPS:              candidate.FPS,
			BitrateKbps:      candidate.BitrateKbps,
			ProfileToken:     candidate.ProfileToken,
			State:            state,
		})
	}
	return streams
}

func roleStreamName(base string, role store.CameraStreamRole) string {
	switch role {
	case store.CameraStreamRoleLive:
		return base + "-live"
	case store.CameraStreamRoleSnapshot:
		return base + "-snapshot"
	default:
		return base + "-recording"
	}
}

func redactDeviceProfile(profile cameraprofile.DeviceProfile) cameraprofile.DeviceProfile {
	for channelIndex := range profile.Channels {
		for candidateIndex := range profile.Channels[channelIndex].Candidates {
			candidate := &profile.Channels[channelIndex].Candidates[candidateIndex]
			if candidate.RedactedURL == "" {
				candidate.RedactedURL = store.RedactURL(candidate.URL)
			}
			candidate.URL = ""
		}
	}
	return profile
}

func sanitizeCameraSecrets(camera *store.Camera) {
	camera.URL = ""
	for i := range camera.Streams {
		if camera.Streams[i].RedactedURL == "" {
			camera.Streams[i].RedactedURL = store.RedactURL(camera.Streams[i].URL)
		}
		camera.Streams[i].URL = ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func cameraByStream(cameras []store.Camera, streamName string) (store.Camera, bool) {
	for _, camera := range cameras {
		if camera.StreamName == streamName || camera.RecordingStreamName == streamName || camera.LiveStreamName == streamName {
			return camera, true
		}
		for _, stream := range camera.Streams {
			if stream.Go2RTCStreamName == streamName {
				return camera, true
			}
		}
	}
	return store.Camera{}, false
}

func spaHandler(files http.FileSystem) http.Handler {
	fileServer := http.FileServer(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || !strings.Contains(filepathBase(r.URL.Path), ".") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.URL.Path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if file, err := files.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
			_ = file.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func filepathBase(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

type previewRegistry struct {
	mu      sync.Mutex
	streams map[string]previewStream
}

type previewStream struct {
	URL       string
	ExpiresAt time.Time
}

func newPreviewRegistry() *previewRegistry {
	return &previewRegistry{streams: make(map[string]previewStream)}
}

func (p *previewRegistry) Put(rawURL string, ttl time.Duration) (string, time.Time) {
	expiresAt := time.Now().Add(ttl)
	name := "camstation-preview-" + randomToken()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streams[name] = previewStream{URL: rawURL, ExpiresAt: expiresAt}
	p.cleanupLocked(time.Now())
	return name, expiresAt
}

func (p *previewRegistry) Resolve(streamName string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.cleanupLocked(now)
	stream, ok := p.streams[streamName]
	if !ok || now.After(stream.ExpiresAt) {
		return "", false
	}
	return stream.URL, true
}

func (p *previewRegistry) cleanupLocked(now time.Time) {
	for name, stream := range p.streams {
		if now.After(stream.ExpiresAt) {
			delete(p.streams, name)
		}
	}
}

func randomToken() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

func go2RTCProxy(previews *previewRegistry) (http.Handler, error) {
	target, err := url.Parse("http://127.0.0.1:1984")
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		r.Host = target.Host
		if r.URL.Path == "/api/ws" {
			r.Header.Set("Origin", target.String())
			query := r.URL.Query()
			if rawURL, ok := previews.Resolve(query.Get("src")); ok {
				query.Set("src", rawURL)
				r.URL.RawQuery = query.Encode()
			}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedGo2RTCPath(r.URL.Path) {
			writeError(w, http.StatusForbidden, fmt.Errorf("go2rtc endpoint is not exposed"))
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

func allowedGo2RTCPath(path string) bool {
	switch {
	case path == "/" || path == "/stream.html":
		return true
	case path == "/video-stream.js" || path == "/video-rtc.js":
		return true
	case path == "/api/ws":
		return true
	case strings.HasPrefix(path, "/icons/"):
		return true
	default:
		return false
	}
}

func streamName(name string, fallback int64) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	value := re.ReplaceAllString(name, "-")
	value = regexp.MustCompile(`-+`).ReplaceAllString(value, "-")
	value = regexp.MustCompile(`^-|-$`).ReplaceAllString(value, "")
	if value == "" {
		value = "camera-" + strconv.FormatInt(fallback, 10)
	}
	return value
}

func toMap(value any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func printProbe(result camera.ProbeResult, err error) {
	payload := map[string]any{"ok": err == nil, "result": result}
	if err != nil {
		payload["error"] = err.Error()
	}
	encoded, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		log.Fatal(marshalErr)
	}
	fmt.Println(string(encoded))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func listenURL(addr string) string {
	switch {
	case strings.HasPrefix(addr, ":"):
		return "http://localhost" + addr
	case strings.HasPrefix(addr, "0.0.0.0:"):
		return "http://localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	default:
		return "http://" + addr
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
