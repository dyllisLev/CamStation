package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"camstation/internal/store"
)

type diagnosticArtifactDTO struct {
	ID        int64      `json:"id"`
	JobID     int64      `json:"jobId"`
	Name      string     `json:"name"`
	SizeBytes int64      `json:"sizeBytes"`
	SHA256    string     `json:"sha256"`
	CreatedAt time.Time  `json:"createdAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

type diagnosticArtifactDeleteResponse struct {
	Deleted  bool                  `json:"deleted"`
	Artifact diagnosticArtifactDTO `json:"artifact"`
}

type diagnosticHistoryDeleteResponse struct {
	Deleted int64 `json:"deleted"`
}

func (d routeDeps) listDiagnosticArtifacts(w http.ResponseWriter, r *http.Request) {
	artifacts, err := d.db.ListDiagnosticArtifacts(r.Context(), false)
	if err != nil {
		writeSystemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicDiagnosticArtifacts(artifacts))
}

func (d routeDeps) deleteDiagnosticArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return
	}
	artifact, err := d.db.GetDiagnosticArtifact(r.Context(), id)
	if err != nil {
		writeSystemError(w, err)
		return
	}
	if !d.safeDiagnosticArtifactPath(artifact.Path) {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return
	}
	_ = os.Remove(artifact.Path)
	deleted, err := d.db.MarkDiagnosticArtifactDeleted(r.Context(), id)
	if err != nil {
		writeSystemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, diagnosticArtifactDeleteResponse{Deleted: true, Artifact: publicDiagnosticArtifact(deleted)})
}

func (d routeDeps) rejectUnsafeArtifactPathDelete(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("path") != "" {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return
	}
	writeError(w, http.StatusBadRequest, store.ErrValidation)
}

func (d routeDeps) deleteDiagnosticHistory(w http.ResponseWriter, r *http.Request) {
	deleted, err := d.db.DeleteDiagnosticArtifactHistory(r.Context())
	if err != nil {
		writeSystemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, diagnosticHistoryDeleteResponse{Deleted: deleted})
}

func (d routeDeps) diagnosticDir() string {
	return filepath.Join(filepath.Dir(d.recordingsDir), "diagnostics")
}

func (d routeDeps) safeDiagnosticArtifactPath(path string) bool {
	rel, err := filepath.Rel(d.diagnosticDir(), path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func writeSystemError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrValidation):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrDiagnosticArtifactNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func publicDiagnosticArtifacts(artifacts []store.DiagnosticArtifact) []diagnosticArtifactDTO {
	out := make([]diagnosticArtifactDTO, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, publicDiagnosticArtifact(artifact))
	}
	return out
}

func publicDiagnosticArtifact(artifact store.DiagnosticArtifact) diagnosticArtifactDTO {
	return diagnosticArtifactDTO{
		ID:        artifact.ID,
		JobID:     artifact.JobID,
		Name:      artifact.Name,
		SizeBytes: artifact.SizeBytes,
		SHA256:    artifact.SHA256,
		CreatedAt: artifact.CreatedAt,
		DeletedAt: artifact.DeletedAt,
	}
}
