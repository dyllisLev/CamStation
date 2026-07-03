package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"camstation/internal/cameraprofile"
)

func (d routeDeps) registerCameraProfileRoutes(mux *http.ServeMux, previews *previewRegistry) {
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
		existing, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
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
		existing, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
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

}
