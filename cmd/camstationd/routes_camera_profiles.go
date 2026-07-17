package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"camstation/internal/cameraprofile"
)

type routeDeviceScanner interface {
	ScanResult(context.Context, cameraprofile.ScanRequest) (cameraprofile.DeviceScanResult, error)
}

var newRouteDeviceScanner = func() routeDeviceScanner {
	return cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient())
}

func (d routeDeps) registerCameraProfileRoutes(mux *http.ServeMux, previews *previewRegistry) {
	mux.HandleFunc("POST /api/cameras/scan", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		var req cameraprofile.ScanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateScanTarget(r.Context(), req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		scanCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		scan, err := newRouteDeviceScanner().ScanResult(scanCtx, req)
		if err != nil {
			writeSafeError(w, http.StatusBadGateway, err)
			return
		}
		response, err := d.scanMatchResponse(r.Context(), scan)
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /api/cameras/preview", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		var req cameraPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateScanTarget(r.Context(), req.ScanRequest); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		scanCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		profile, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(scanCtx, req.ScanRequest)
		if err != nil {
			writeSafeError(w, http.StatusBadGateway, err)
			return
		}
		candidates := selectProfileCandidates(profile, req.ChannelIndexValue(), []cameraStreamSelection{{
			Role:         req.Role,
			ProfileToken: req.ProfileToken,
		}})
		if len(candidates) == 0 || candidates[0].URL == "" {
			writeSafeError(w, http.StatusBadRequest, fmt.Errorf("preview profile candidate not found"))
			return
		}
		streamName, expiresAt := previews.Put(candidates[0].URL, 10*time.Minute)
		writeJSON(w, http.StatusOK, map[string]any{
			"streamName": streamName,
			"expiresAt":  expiresAt.UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /api/cameras/{streamName}/scan", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		existing, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeSafeError(w, http.StatusNotFound, err)
			return
		}
		if !existing.Enabled {
			writeCameraDisabled(w)
			return
		}
		var incoming cameraCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		req := cameraUpdateRequest(existing, incoming)
		if incoming.URL == "" {
			req.URL = ""
		}
		scanReq := scanRequestFromCamera(req)
		if err := validateScanTarget(r.Context(), scanReq); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		scanCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		scan, err := newRouteDeviceScanner().ScanResult(scanCtx, scanReq)
		if err != nil {
			writeSafeError(w, http.StatusBadGateway, err)
			return
		}
		response, err := d.scanMatchResponse(r.Context(), scan)
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /api/cameras/{streamName}/preview", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		existing, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeSafeError(w, http.StatusNotFound, err)
			return
		}
		if !existing.Enabled {
			writeCameraDisabled(w)
			return
		}
		var req cameraPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		if !scanReqHasTarget(req.ScanRequest) {
			writeSafeError(w, http.StatusBadRequest, errUnsafeCameraTarget)
			return
		}
		req = previewRequestWithExisting(existing, req)
		if err := validateScanTarget(r.Context(), req.ScanRequest); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		scanCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		profile, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(scanCtx, req.ScanRequest)
		if err != nil {
			writeSafeError(w, http.StatusBadGateway, err)
			return
		}
		candidates := selectProfileCandidates(profile, req.ChannelIndexValue(), []cameraStreamSelection{{
			Role:         req.Role,
			ProfileToken: req.ProfileToken,
		}})
		if len(candidates) == 0 || candidates[0].URL == "" {
			writeSafeError(w, http.StatusBadRequest, fmt.Errorf("preview profile candidate not found"))
			return
		}
		streamName, expiresAt := previews.Put(candidates[0].URL, 10*time.Minute)
		writeJSON(w, http.StatusOK, map[string]any{
			"streamName": streamName,
			"expiresAt":  expiresAt.UTC().Format(time.RFC3339),
		})
	})

}
