package main

import (
	"context"
	"embed"
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
	"syscall"
	"time"

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
	cleaner := cleanup.New(db, *recordingsDir)
	maxStorageBytes := int64(*maxStorageGB * 1024 * 1024 * 1024)
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
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new/", http.StatusMovedPermanently)
	})
	mux.Handle("/new/", http.StripPrefix("/new", mux))

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
		segments, err := db.ListRecordingSegments(r.Context(), streamName, from, to, "ready", "recording")
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

	mux.HandleFunc("POST /api/cameras", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("url is required"))
			return
		}
		if req.Name == "" {
			req.Name = "Camera 1"
		}

		result, probeErr := prober.Probe(r.Context(), req.URL, 12*time.Second)
		state := "streaming"
		level := "info"
		message := "camera registered"
		if probeErr != nil {
			state = "offline"
			level = "error"
			message = "camera registered but probe failed"
		}

		saved, err := db.UpsertCamera(r.Context(), store.Camera{
			Name:          req.Name,
			URL:           req.URL,
			StreamName:    streamName(req.Name, 1),
			State:         state,
			LastProbeJSON: toMap(result),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		saved.URL = ""

		_ = db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   level,
			Message: message,
			Details: map[string]any{
				"name":   saved.Name,
				"stream": saved.StreamName,
				"state":  saved.State,
				"result": result,
				"error":  errString(probeErr),
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
				"camera":  saved,
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
		writeJSON(w, status, map[string]any{"ok": probeErr == nil, "camera": saved, "go2rtc": streamer.Status(r.Context())})
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

	liveProxy, err := go2RTCProxy()
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

func cameraByStream(cameras []store.Camera, streamName string) (store.Camera, bool) {
	for _, camera := range cameras {
		if camera.StreamName == streamName {
			return camera, true
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

func go2RTCProxy() (http.Handler, error) {
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
