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
	"os"
	"os/signal"
	"syscall"
	"time"

	"camstation/internal/camera"
	"camstation/internal/store"
)

//go:embed web/*
var webFS embed.FS

func main() {
	var (
		addr         = flag.String("addr", getenv("CAMSTATION_ADDR", ":18080"), "HTTP listen address")
		dbPath       = flag.String("db", getenv("CAMSTATION_DB", "./data/camstation.db"), "SQLite database path")
		cameraURL    = flag.String("camera-url", getenv("CAMSTATION_CAMERA_URL", ""), "single camera URL for smoke testing")
		probeOnly    = flag.Bool("probe-only", false, "run one camera probe and exit")
		probeOnStart = flag.Bool("probe-on-start", false, "probe CAMSTATION_CAMERA_URL during startup")
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

	mux, err := routes(db, prober)
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("camstationd listening on http://localhost%s", *addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

func routes(db *store.DB, prober camera.Prober) (http.Handler, error) {
	mux := http.NewServeMux()

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
	mux.Handle("/", http.FileServer(http.FS(static)))
	return mux, nil
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

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
