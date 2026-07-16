package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"camstation/internal/store"
	"camstation/internal/viewerrelease"
)

const (
	viewerHeartbeatTTL         = 30 * time.Second
	viewerUpdateHealthyFor     = 30 * time.Second
	viewerUpdateHealthMaxGap   = 15 * time.Second
	viewerControlPollInterval  = time.Second
	viewerControlKeepaliveTime = 9 * time.Second
	viewerLongPollMaxWait      = 25 * time.Second
)

type viewerDeleteResponse struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

type viewerHeartbeatResponse struct {
	Viewer         store.Viewer                  `json:"viewer"`
	DesiredRelease *viewerDesiredReleaseResponse `json:"desiredRelease"`
	CommitToken    string                        `json:"commitToken,omitempty"`
}

type viewerDesiredReleaseResponse struct {
	viewerrelease.Release
	DownloadURL string                   `json:"downloadUrl"`
	CommandID   int64                    `json:"commandId"`
	PayloadHash string                   `json:"payloadHash"`
	Generation  int64                    `json:"generation"`
	TTLSeconds  int                      `json:"ttlSeconds"`
	State       store.ViewerCommandState `json:"commandState"`
	CreatedAt   time.Time                `json:"createdAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
	ExpiresAt   time.Time                `json:"expiresAt"`
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
		desiredRelease, err := d.desiredViewerRelease(r, req, viewer)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		commitToken, err := d.viewerUpdateCommitToken(r.Context(), req, desiredRelease, time.Now().UTC())
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
		writeJSON(w, http.StatusOK, viewerHeartbeatResponse{
			Viewer:         viewer,
			DesiredRelease: desiredRelease,
			CommitToken:    commitToken,
		})
	})

	mux.HandleFunc("GET /api/viewers", func(w http.ResponseWriter, r *http.Request) {
		viewers, err := d.db.ListViewers(r.Context(), viewerHeartbeatTTL)
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
		viewer, err := d.db.GetViewer(r.Context(), id, viewerHeartbeatTTL)
		if err != nil {
			writeViewerError(w, err)
			return
		}
		if viewer.Status != "stale" && viewer.Status != "offline" {
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
		commands, err := d.db.ListViewerCommands(r.Context(), r.PathValue("id"))
		if err != nil {
			writeViewerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, commands)
	})

	mux.HandleFunc("GET /api/viewers/{id}/commands/next", func(w http.ResponseWriter, r *http.Request) {
		wait, err := viewerLongPollWait(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := d.handleViewerLongPoll(w, r, wait); err != nil {
			writeViewerError(w, err)
		}
	})

	mux.HandleFunc("GET /api/viewers/{id}/control", d.handleViewerControl)

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

func (d routeDeps) desiredViewerRelease(r *http.Request, heartbeat store.ViewerHeartbeat, viewer store.Viewer) (*viewerDesiredReleaseResponse, error) {
	release, err := d.viewerReleases.Current(r.Context())
	if err != nil {
		return nil, nil
	}
	if viewerReportsRelease(viewer, heartbeat.Agent.ArtifactSHA256, release) {
		existing, found, findErr := d.db.FindViewerUpdateCommand(r.Context(), viewer.ID, release.Version, release.SHA256)
		if findErr != nil {
			return nil, findErr
		}
		if !found || !viewerUpdateCommandInProgress(existing.State) {
			return nil, nil
		}
		return desiredViewerReleaseResponse(release, existing), nil
	}
	command, err := d.db.EnsureViewerUpdateCommand(r.Context(), viewer.ID, release.Version, release.SHA256)
	if err != nil {
		return nil, err
	}
	return desiredViewerReleaseResponse(release, command), nil
}

func desiredViewerReleaseResponse(release viewerrelease.Release, command store.ViewerCommand) *viewerDesiredReleaseResponse {
	return &viewerDesiredReleaseResponse{
		Release:     release,
		DownloadURL: release.DownloadURL(),
		CommandID:   command.ID,
		PayloadHash: command.PayloadHash,
		Generation:  command.Generation,
		TTLSeconds:  command.TTLSeconds,
		State:       command.State,
		CreatedAt:   command.CreatedAt,
		UpdatedAt:   command.UpdatedAt,
		ExpiresAt:   command.CreatedAt.Add(time.Duration(command.TTLSeconds) * time.Second),
	}
}

func (d routeDeps) viewerUpdateCommitToken(ctx context.Context, heartbeat store.ViewerHeartbeat, desired *viewerDesiredReleaseResponse, now time.Time) (string, error) {
	if desired == nil {
		return "", d.db.ResetViewerUpdateValidation(ctx, heartbeat.ID)
	}
	command, err := d.db.GetViewerCommand(ctx, heartbeat.ID, desired.CommandID)
	if err != nil {
		return "", err
	}
	if command.State != store.ViewerCommandRunning && viewerUpdateCommandInProgress(command.State) &&
		viewerUpdateHeartbeatIdentityMatches(heartbeat, command) &&
		heartbeat.Update.TransactionID == expectedViewerUpdateOperationKey(command) {
		command, err = d.db.ApplyViewerCommandResult(ctx, heartbeat.ID, command.ID, store.ViewerCommandResult{
			State: store.ViewerCommandRunning, OperationKey: heartbeat.Update.TransactionID,
		})
		if err != nil {
			return "", err
		}
	}
	observation, exact := viewerUpdateValidationObservation(heartbeat, command, now)
	if !exact {
		return "", d.db.ResetViewerUpdateValidation(ctx, heartbeat.ID)
	}
	return d.db.ObserveViewerUpdateValidation(ctx, observation, now, viewerUpdateHealthyFor, viewerUpdateHealthMaxGap)
}

func viewerUpdateValidationObservation(heartbeat store.ViewerHeartbeat, command store.ViewerCommand, now time.Time) (store.ViewerUpdateValidationObservation, bool) {
	transactionID := strings.TrimSpace(heartbeat.Update.TransactionID)
	artifactSHA256 := strings.ToLower(strings.TrimSpace(heartbeat.Update.ArtifactSHA256))
	exact := command.State == store.ViewerCommandRunning && command.OperationKey != "" &&
		viewerUpdateHeartbeatIdentityMatches(heartbeat, command) &&
		transactionID == command.OperationKey && artifactSHA256 == command.ArtifactSHA256
	observation := store.ViewerUpdateValidationObservation{
		ViewerID: heartbeat.ID, CommandID: command.ID, PayloadHash: command.PayloadHash,
		TransactionID: transactionID, Generation: command.Generation, TargetVersion: command.DesiredVersion,
		ArtifactSHA256: command.ArtifactSHA256,
	}
	if !exact {
		return observation, false
	}
	exactAgent := heartbeat.Agent.State == "online" && heartbeat.Agent.Version == command.DesiredVersion &&
		strings.EqualFold(heartbeat.Agent.ArtifactSHA256, command.ArtifactSHA256)
	observation.Healthy = exactAgent && heartbeat.Control.State == "online" && freshViewerUpdateSignal(heartbeat.Control.LastSuccessAt, now) &&
		heartbeat.Viewer.State == "running" && freshViewerUpdateSignal(heartbeat.Viewer.LastHeartbeatAt, now) &&
		heartbeat.Renderer.State == "ready" && freshViewerUpdateSignal(heartbeat.Renderer.LastHeartbeatAt, now)
	return observation, true
}

func freshViewerUpdateSignal(at *time.Time, now time.Time) bool {
	return at != nil && !at.After(now.Add(5*time.Second)) && now.Sub(*at) <= viewerUpdateHealthMaxGap
}

func viewerUpdateCommandInProgress(state store.ViewerCommandState) bool {
	switch state {
	case store.ViewerCommandPending, store.ViewerCommandDelivered, store.ViewerCommandAcknowledged, store.ViewerCommandRunning:
		return true
	default:
		return false
	}
}

func expectedViewerUpdateOperationKey(command store.ViewerCommand) string {
	return fmt.Sprintf("update-%s-%s-%d", command.DesiredVersion, strings.ToLower(command.ArtifactSHA256), command.Generation)
}

func viewerUpdateHeartbeatIdentityMatches(heartbeat store.ViewerHeartbeat, command store.ViewerCommand) bool {
	return command.Type == "update_app" && heartbeat.ID == command.ViewerID &&
		heartbeat.Update.State == "installer_launched" && heartbeat.Update.CommandID == command.ID &&
		heartbeat.Update.PayloadHash == command.PayloadHash &&
		heartbeat.Update.Generation == command.Generation && heartbeat.Update.TargetVersion == command.DesiredVersion &&
		strings.EqualFold(heartbeat.Update.ArtifactSHA256, command.ArtifactSHA256)
}

func viewerReportsRelease(viewer store.Viewer, artifactSHA256 string, release viewerrelease.Release) bool {
	reportedVersion := false
	for _, version := range []string{viewer.AppVersion, viewer.Agent.Version} {
		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}
		reportedVersion = true
		if version != release.Version {
			return false
		}
	}
	if !reportedVersion {
		return false
	}
	return strings.TrimSpace(artifactSHA256) == release.SHA256
}

func (d routeDeps) handleViewerControl(w http.ResponseWriter, r *http.Request) {
	if _, err := d.db.GetViewer(r.Context(), r.PathValue("id"), viewerHeartbeatTTL); err != nil {
		writeViewerError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	lastCommandID := int64(0)
	writeAvailable := func() (bool, error) {
		command, found, err := d.db.DeliverNextViewerCommand(r.Context(), r.PathValue("id"))
		if err != nil || !found || command.ID == lastCommandID {
			return false, err
		}
		encoded, err := json.Marshal(command)
		if err != nil {
			return false, err
		}
		if _, err := fmt.Fprintf(w, "event: command\ndata: %s\n\n", encoded); err != nil {
			return false, err
		}
		lastCommandID = command.ID
		flusher.Flush()
		return true, nil
	}
	written, err := writeAvailable()
	if err != nil {
		return
	}
	if !written {
		if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
			return
		}
		flusher.Flush()
	}

	poll := time.NewTicker(viewerControlPollInterval)
	keepalive := time.NewTicker(viewerControlKeepaliveTime)
	defer poll.Stop()
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			if _, err := writeAvailable(); err != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (d routeDeps) handleViewerLongPoll(w http.ResponseWriter, r *http.Request, wait time.Duration) error {
	deliver := func() (bool, error) {
		command, found, err := d.db.DeliverNextViewerCommand(r.Context(), r.PathValue("id"))
		if err != nil || !found {
			return false, err
		}
		writeJSON(w, http.StatusOK, command)
		return true, nil
	}
	if delivered, err := deliver(); err != nil || delivered {
		return err
	}
	if wait == 0 {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	timer := time.NewTimer(wait)
	poll := time.NewTicker(viewerControlPollInterval)
	defer timer.Stop()
	defer poll.Stop()
	for {
		select {
		case <-r.Context().Done():
			return r.Context().Err()
		case <-timer.C:
			w.WriteHeader(http.StatusNoContent)
			return nil
		case <-poll.C:
			if delivered, err := deliver(); err != nil || delivered {
				return err
			}
		}
	}
}

func viewerLongPollWait(r *http.Request) (time.Duration, error) {
	raw := r.URL.Query().Get("wait")
	if raw == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 0, store.ErrValidation
	}
	if seconds >= int(viewerLongPollMaxWait/time.Second) {
		return viewerLongPollMaxWait, nil
	}
	wait := time.Duration(seconds) * time.Second
	return wait, nil
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
