package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"camstation/internal/store"
)

type viewerDeleteResponse struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

func (d routeDeps) registerViewerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/viewers/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var req store.ViewerHeartbeat
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		viewer, err := d.db.UpsertViewerHeartbeat(r.Context(), req)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "viewer",
			Level:   "info",
			Message: "viewer heartbeat",
			Details: map[string]any{"viewerId": viewer.ID, "route": viewer.Route, "mode": viewer.Mode},
		})
		writeJSON(w, http.StatusOK, viewer)
	})

	mux.HandleFunc("GET /api/viewers", func(w http.ResponseWriter, r *http.Request) {
		viewers, err := d.db.ListViewers(r.Context(), 90*time.Second)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, viewers)
	})

	mux.HandleFunc("PATCH /api/viewers/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req store.ViewerUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		viewer, err := d.db.UpdateViewer(r.Context(), r.PathValue("id"), req)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, viewer)
	})

	mux.HandleFunc("DELETE /api/viewers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		viewer, err := d.db.GetViewer(r.Context(), id, 90*time.Second)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		if viewer.Status != "stale" {
			writeError(w, http.StatusConflict, store.ErrValidation)
			return
		}
		if err := d.db.DeleteViewer(r.Context(), id); err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, viewerDeleteResponse{Deleted: true, ID: id})
	})

	mux.HandleFunc("POST /api/viewers/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		var req store.ViewerCommandCreate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		command, err := d.db.CreateViewerCommand(r.Context(), r.PathValue("id"), req)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, command)
	})

	mux.HandleFunc("GET /api/viewers/{id}/commands", func(w http.ResponseWriter, r *http.Request) {
		commands, err := d.db.DequeueViewerCommands(r.Context(), r.PathValue("id"))
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, commands)
	})

	mux.HandleFunc("PATCH /api/viewers/{id}/commands/{commandID}", func(w http.ResponseWriter, r *http.Request) {
		commandID, ok := pathCommandID(w, r)
		if !ok {
			return
		}
		var req store.ViewerCommandUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		command, err := d.db.UpdateViewerCommand(r.Context(), r.PathValue("id"), commandID, req)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, command)
	})

	mux.HandleFunc("POST /api/viewers/{id}/commands/{commandID}/cancel", func(w http.ResponseWriter, r *http.Request) {
		commandID, ok := pathCommandID(w, r)
		if !ok {
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		command, err := d.db.CancelViewerCommand(r.Context(), r.PathValue("id"), commandID, req.Reason)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, command)
	})

	mux.HandleFunc("DELETE /api/viewers/{id}/commands/{commandID}", func(w http.ResponseWriter, r *http.Request) {
		commandID, ok := pathCommandID(w, r)
		if !ok {
			return
		}
		command, err := d.db.DeleteViewerCommand(r.Context(), r.PathValue("id"), commandID)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, command)
	})
}

func pathCommandID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("commandID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return 0, false
	}
	return id, true
}

func writeViewerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrValidation):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrViewerNotFound), errors.Is(err, store.ErrViewerCommandNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}
