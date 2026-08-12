package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"camstation/internal/store"
)

type systemStatusDTO struct {
	Daemon systemDaemonDTO    `json:"daemon"`
	Go2RTC publicStreamStatus `json:"go2rtc"`
	FFmpeg systemBinaryDTO    `json:"ffmpeg"`
	System systemHostDTO      `json:"system"`
}

type systemDaemonDTO struct {
	Running bool      `json:"running"`
	Now     time.Time `json:"now"`
}

type systemBinaryDTO struct {
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}

type systemHostDTO struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	CPUs       int    `json:"cpus"`
	Goroutines int    `json:"goroutines"`
}

type diagnosticRequest struct {
	Reason string `json:"reason"`
}

type diagnosticResponse struct {
	Job      store.Job             `json:"job"`
	Artifact diagnosticArtifactDTO `json:"artifact"`
}

type maintenanceRequest struct {
	Action   string `json:"action"`
	Deferred bool   `json:"defer"`
	MaxBytes int64  `json:"maxBytes"`
}

func (d routeDeps) registerSystemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, d.systemStatus(r.Context()))
	})
	mux.HandleFunc("POST /api/system/diagnostics", d.createDiagnosticJob)
	mux.HandleFunc("GET /api/system/jobs", d.listSystemJobs)
	mux.HandleFunc("POST /api/system/maintenance", d.createMaintenanceJob)
	mux.HandleFunc("POST /api/system/jobs/{id}/cancel", d.cancelSystemJob)
	mux.HandleFunc("GET /api/system/diagnostics/artifacts", d.listDiagnosticArtifacts)
	mux.HandleFunc("DELETE /api/system/diagnostics/artifacts/{id}", d.deleteDiagnosticArtifact)
	mux.HandleFunc("DELETE /api/system/diagnostics/artifacts", d.rejectUnsafeArtifactPathDelete)
	mux.HandleFunc("DELETE /api/system/diagnostics/history", d.deleteDiagnosticHistory)
}

func (d routeDeps) systemStatus(ctx context.Context) systemStatusDTO {
	return systemStatusDTO{
		Daemon: systemDaemonDTO{Running: true, Now: time.Now().UTC()},
		Go2RTC: publicGo2RTCStatus(d.streamer.Status(ctx)),
		FFmpeg: binaryStatus("ffmpeg"),
		System: systemHostDTO{
			GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(), Goroutines: runtime.NumGoroutine(),
		},
	}
}

func binaryStatus(name string) systemBinaryDTO {
	if _, err := exec.LookPath(name); err != nil {
		return systemBinaryDTO{Installed: false, Error: store.RedactText(err.Error())}
	}
	return systemBinaryDTO{Installed: true}
}

func (d routeDeps) createDiagnosticJob(w http.ResponseWriter, r *http.Request) {
	var req diagnosticRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	job, err := d.db.CreateJob(r.Context(), store.JobCreate{Kind: "system.diagnostics", SingleFlightKey: "system.diagnostics", TimeoutSeconds: 15})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	job, err = d.db.StartJob(r.Context(), job.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	artifact, err := d.writeDiagnosticArtifact(r.Context(), job.ID, req)
	if err != nil {
		job, _ = d.db.FailJob(r.Context(), job.ID, err.Error(), nil)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	job, err = d.db.SucceedJob(r.Context(), job.ID, map[string]any{"artifactId": artifact.ID, "artifactName": artifact.Name})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, diagnosticResponse{Job: job, Artifact: publicDiagnosticArtifact(artifact)})
}

func (d routeDeps) writeDiagnosticArtifact(ctx context.Context, jobID int64, req diagnosticRequest) (store.DiagnosticArtifact, error) {
	dir := d.diagnosticDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return store.DiagnosticArtifact{}, fmt.Errorf("create diagnostics dir: %w", err)
	}
	name := fmt.Sprintf("diagnostic-%d.json", jobID)
	path := filepath.Join(dir, name)
	payload := struct {
		CreatedAt time.Time       `json:"createdAt"`
		Reason    string          `json:"reason"`
		Status    systemStatusDTO `json:"status"`
	}{
		CreatedAt: time.Now().UTC(),
		Reason:    store.RedactText(req.Reason),
		Status:    d.systemStatus(ctx),
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return store.DiagnosticArtifact{}, fmt.Errorf("encode diagnostics: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return store.DiagnosticArtifact{}, fmt.Errorf("write diagnostics: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return d.db.CreateDiagnosticArtifact(ctx, store.DiagnosticArtifactCreate{
		JobID: jobID, Name: name, Path: path, SizeBytes: int64(len(encoded)), SHA256: hex.EncodeToString(sum[:]),
	})
}

func (d routeDeps) listSystemJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := d.db.ListJobs(r.Context(), 100)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	filtered := make([]store.Job, 0, len(jobs))
	for _, job := range jobs {
		if strings.HasPrefix(job.Kind, "system.") {
			filtered = append(filtered, job)
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (d routeDeps) createMaintenanceJob(w http.ResponseWriter, r *http.Request) {
	var req maintenanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	if req.Action != "health_check" && req.Action != "db_vacuum" && req.Action != "recording_cleanup" {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return
	}
	job, err := d.db.CreateJob(r.Context(), store.JobCreate{Kind: "system.maintenance." + req.Action, SingleFlightKey: "system.maintenance." + req.Action, TimeoutSeconds: 15})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if req.Deferred {
		writeJSON(w, http.StatusCreated, job)
		return
	}
	job, err = d.runMaintenanceJob(r.Context(), job.ID, req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (d routeDeps) runMaintenanceJob(ctx context.Context, jobID int64, req maintenanceRequest) (store.Job, error) {
	job, err := d.db.StartJob(ctx, jobID)
	if err != nil {
		return store.Job{}, err
	}
	result := map[string]any{"action": req.Action}
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	switch req.Action {
	case "health_check":
		result["ok"] = true
	case "db_vacuum":
		if err := d.db.Vacuum(runCtx); err != nil {
			return d.db.FailJob(ctx, job.ID, err.Error(), result)
		}
		result["vacuumed"] = true
	case "recording_cleanup":
		maxBytes := req.MaxBytes
		if maxBytes <= 0 {
			maxBytes = d.maxStorageBytes
		}
		if maxBytes <= 0 {
			return d.db.FailJob(ctx, job.ID, "maxBytes is required for recording cleanup", result)
		}
		cleanupResult, err := d.cleaner.EnforceMaxBytes(runCtx, maxBytes)
		if err != nil {
			return d.db.FailJob(ctx, job.ID, err.Error(), result)
		}
		result["beforeBytes"] = cleanupResult.BeforeBytes
		result["afterBytes"] = cleanupResult.AfterBytes
		result["deleted"] = len(cleanupResult.Deleted)
	}
	return d.db.SucceedJob(ctx, job.ID, result)
}

func (d routeDeps) cancelSystemJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathJobID(w, r)
	if !ok {
		return
	}
	job, err := d.db.CancelJob(r.Context(), id, "operator cancelled")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}
