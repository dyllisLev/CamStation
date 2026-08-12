package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"camstation/internal/store"
)

func (d routeDeps) registerSettingsJobRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		settings, err := d.db.GetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	})

	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var req store.SettingsUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		settings, err := d.db.UpdateSettings(r.Context(), req)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	})

	mux.HandleFunc("POST /api/settings/reset", func(w http.ResponseWriter, r *http.Request) {
		settings, err := d.db.ResetSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	})

	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := d.db.ListJobs(r.Context(), 100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	})

	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		var req store.JobCreate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		job, err := d.db.CreateJob(r.Context(), req)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, job)
	})

	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		job, err := d.db.GetJob(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/jobs/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		job, err := d.db.StartJob(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/jobs/{id}/succeed", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		req := struct {
			Result map[string]any `json:"result"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		job, err := d.db.SucceedJob(r.Context(), id, req.Result)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/jobs/{id}/fail", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		req := struct {
			Error  string         `json:"error"`
			Result map[string]any `json:"result"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		job, err := d.db.FailJob(r.Context(), id, req.Error, req.Result)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
			return
		}
		req := struct {
			Reason string `json:"reason"`
		}{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		job, err := d.db.CancelJob(r.Context(), id, req.Reason)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("DELETE /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathJobID(w, r)
		if !ok {
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

func pathJobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return 0, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrValidation):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrJobAlreadyActive):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrJobNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}
