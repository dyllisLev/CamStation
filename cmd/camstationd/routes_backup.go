package main

import (
	"encoding/json"
	"net/http"

	"camstation/internal/backup"
	"camstation/internal/store"
)

var buildBackupRunner = func(db *store.DB) *backup.Runner {
	return backup.NewRunner(db)
}

func (d routeDeps) registerBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/backup/config", func(w http.ResponseWriter, r *http.Request) {
		settings, err := d.db.GetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, settings.Backup)
	})

	mux.HandleFunc("PUT /api/backup/config", func(w http.ResponseWriter, r *http.Request) {
		var req store.BackupSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := d.db.UpdateBackupSettings(r.Context(), req); err != nil {
			writeStoreError(w, err)
			return
		}
		settings, err := d.db.GetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, settings.Backup)
	})

	mux.HandleFunc("GET /api/backup/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := d.backupRunner.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("GET /api/backup/jobs", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := d.db.ListJobsByKind(r.Context(), backup.JobKind, 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	})

	mux.HandleFunc("POST /api/backup/jobs", func(w http.ResponseWriter, r *http.Request) {
		var req backup.StartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		req = d.defaultBackupStartRequest(req)
		if err := backup.ValidateStartRequestBoundary(req); err != nil {
			writeStoreError(w, err)
			return
		}
		job, err := d.backupRunner.Start(r.Context(), req)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, job)
	})

	mux.HandleFunc("GET /api/backup/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		job, ok := d.backupJobByID(w, r, id)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/backup/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		if _, ok := d.backupJobByID(w, r, id); !ok {
			return
		}
		job, err := d.backupRunner.Cancel(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/backup/jobs/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		job, err := d.backupRunner.Retry(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, job)
	})

	mux.HandleFunc("DELETE /api/backup/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		job, ok := d.backupJobByID(w, r, id)
		if !ok {
			return
		}
		if job.State == store.JobStateQueued || job.State == store.JobStateRunning {
			writeError(w, http.StatusConflict, store.ErrJobAlreadyActive)
			return
		}
		job, err := d.db.DeleteJob(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})
}

func (d routeDeps) defaultBackupStartRequest(req backup.StartRequest) backup.StartRequest {
	if req.Source == "" || req.Source == "recordings" {
		req.Source = d.recordingsDir
	}
	return req
}

func (d routeDeps) backupJobByID(w http.ResponseWriter, r *http.Request, id int64) (store.Job, bool) {
	job, err := d.db.GetJob(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return store.Job{}, false
	}
	if job.Kind != backup.JobKind {
		writeStoreError(w, store.ErrValidation)
		return store.Job{}, false
	}
	return job, true
}
