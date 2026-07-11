package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"camstation/internal/camera"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type updateStreamOutputsRequest struct {
	ExpectedDesiredRevision int64                        `json:"expectedDesiredRevision"`
	Outputs                 []publicStreamOutputSettings `json:"outputs"`
}

func (d routeDeps) registerCameraStreamOutputRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/cameras/{streamName}/stream-outputs", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		var req updateStreamOutputsRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validatePublicOutputSourceKeys(req.Outputs); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		camera, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeSafeError(w, http.StatusNotFound, err)
			return
		}
		camera.Outputs = make([]store.CameraOutput, 0, len(req.Outputs))
		for _, output := range req.Outputs {
			camera.Outputs = append(camera.Outputs, store.CameraOutput{
				Purpose: output.Purpose, SourceKey: output.SourceKey, VideoMode: output.VideoMode,
				MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS,
				AudioMode: output.AudioMode, Activation: output.Activation,
			})
		}
		expected := req.ExpectedDesiredRevision
		if _, err := d.db.SaveCameraConfiguration(r.Context(), camera, &expected); err != nil {
			switch {
			case errors.Is(err, store.ErrDesiredRevisionMismatch):
				writeSafeError(w, http.StatusConflict, err)
			case isCameraPolicyValidationError(err):
				writeSafeError(w, http.StatusBadRequest, err)
			default:
				writeSafeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		d.writePolicyApplyResponse(w, r, camera.StreamName, true)
	})

	mux.HandleFunc("POST /api/cameras/{streamName}/stream-outputs/probe", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		cameraRow, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeSafeError(w, http.StatusNotFound, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		if err := d.probeInputs(r.Context(), cameraRow); err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		result := d.applyPolicies(r.Context())
		if err := d.probeOutputs(r.Context(), cameraRow.StreamName); err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		d.writePolicyMutationResponse(w, r, cameraRow.StreamName, true, result)
	})

	mux.HandleFunc("POST /api/cameras/{streamName}/stream-outputs/reapply", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		cameraRow, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeSafeError(w, http.StatusNotFound, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		result := d.applyPolicies(r.Context())
		if err := d.probeOutputs(r.Context(), cameraRow.StreamName); err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		d.writePolicyMutationResponse(w, r, cameraRow.StreamName, false, result)
	})

	mux.HandleFunc("POST /api/cameras/stream-outputs/probe", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		for _, cameraRow := range cameras {
			if err := d.probeInputs(r.Context(), cameraRow); err != nil {
				writeSafeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		result := d.applyPolicies(r.Context())
		for _, cameraRow := range cameras {
			if err := d.probeOutputs(r.Context(), cameraRow.StreamName); err != nil {
				writeSafeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		fresh, err := d.db.ListCameras(r.Context(), false)
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		status := d.streamer.Status(r.Context())
		code, warning := policyApplyHTTPStatus(result, fresh)
		response := map[string]any{"saved": true, "applied": result.Applied, "cameras": publicCameras(fresh, status)}
		if warning != "" {
			response["warning"] = warning
		}
		writeJSON(w, code, response)
	})
}

func validatePublicOutputSourceKeys(outputs []publicStreamOutputSettings) error {
	for _, output := range outputs {
		if output.SourceKey != "recording" && output.SourceKey != "live" {
			return fmt.Errorf("invalid camera input source key")
		}
	}
	return nil
}

func isCameraPolicyValidationError(err error) bool {
	if err == nil {
		return false
	}
	for _, marker := range []string{"exactly three", "invalid output", "duplicate output", "width and height", "dimensions are invalid", "fps is invalid", "cannot resize", "source does not belong"} {
		if strings.Contains(err.Error(), marker) {
			return true
		}
	}
	return false
}

func (d routeDeps) writePolicyApplyResponse(w http.ResponseWriter, r *http.Request, streamName string, saved bool) {
	result := d.applyPolicies(r.Context())
	d.writePolicyMutationResponse(w, r, streamName, saved, result)
}

func (d routeDeps) applyPolicies(ctx context.Context) stream.PolicyApplyResult {
	if d.policyApplier == nil {
		return stream.PolicyApplyResult{Error: "stream policy apply coordinator unavailable"}
	}
	return d.policyApplier.Apply(ctx)
}

func (d routeDeps) writePolicyMutationResponse(w http.ResponseWriter, r *http.Request, streamName string, saved bool, result stream.PolicyApplyResult) {
	camera, err := d.db.GetCameraByStream(r.Context(), streamName)
	if err != nil {
		writeSafeError(w, http.StatusInternalServerError, err)
		return
	}
	status := d.streamer.Status(r.Context())
	response := map[string]any{"saved": saved, "applied": result.Applied, "camera": publicCameraFromStore(camera, status)}
	code, warning := policyApplyHTTPStatus(result, []store.Camera{camera})
	if warning != "" {
		response["warning"] = warning
	}
	writeJSON(w, code, response)
}

func policyApplyHTTPStatus(result stream.PolicyApplyResult, cameras []store.Camera) (int, string) {
	if result.Applied {
		if result.Error != "" {
			return http.StatusOK, "stream settings are active; a runtime status check reported a warning"
		}
		return http.StatusOK, ""
	}
	if result.RecoveryFailed {
		return http.StatusServiceUnavailable, "stream settings were saved but runtime recovery could not be verified"
	}
	if len(cameras) == 0 {
		return http.StatusServiceUnavailable, "camera deletion was saved but runtime removal could not be verified"
	}
	for _, cameraRow := range cameras {
		if cameraRow.PolicyState.AppliedRevision == 0 {
			return http.StatusServiceUnavailable, "stream settings were saved but no verified previous runtime could be restored"
		}
	}
	return http.StatusAccepted, "stream settings were saved but runtime apply is pending; the previous applied stream remains active"
}

func (d routeDeps) probeInputs(ctx context.Context, cameraRow store.Camera) error {
	if d.prober == nil {
		return fmt.Errorf("camera prober unavailable")
	}
	for i := range cameraRow.Streams {
		input := &cameraRow.Streams[i]
		checkedAt := time.Now().UTC()
		input.DetectedVideoCodec, input.DetectedAudioCodec, input.DetectedProfile, input.DetectedLevel = "", "", "", ""
		input.DetectedPixelFormat, input.DetectedBitDepth, input.DetectedWidth, input.DetectedHeight, input.DetectedFPS = "", 0, 0, 0, 0
		if err := validateStoredProbeTarget(ctx, input.URL); err != nil {
			input.DetectedCheckedAt, input.DetectedError = checkedAt, "unsafe stored camera target"
			continue
		}
		result, err := d.prober.Probe(ctx, input.URL, 12*time.Second)
		if !result.CheckedAt.IsZero() {
			checkedAt = result.CheckedAt
		}
		input.DetectedCheckedAt = checkedAt
		if err != nil {
			input.DetectedError = store.RedactText(camera.RedactText(err.Error(), input.URL))
			continue
		}
		input.DetectedError = ""
		for _, media := range result.Streams {
			switch media.Type {
			case "video":
				if input.DetectedVideoCodec == "" {
					input.DetectedVideoCodec, input.DetectedProfile, input.DetectedLevel = media.Codec, media.Profile, media.Level
					input.DetectedPixelFormat, input.DetectedBitDepth = media.PixelFormat, media.BitDepth
					input.DetectedWidth, input.DetectedHeight, input.DetectedFPS = media.Width, media.Height, media.FPS
				}
			case "audio":
				if input.DetectedAudioCodec == "" {
					input.DetectedAudioCodec = media.Codec
				}
			}
		}
	}
	return d.db.UpdateCameraStreamDetections(ctx, cameraRow.ID, cameraRow.Streams)
}

func (d routeDeps) probeOutputs(ctx context.Context, streamName string) error {
	cameraRow, err := d.db.GetCameraByStream(ctx, streamName)
	if err != nil {
		return err
	}
	verifications := make(map[store.CameraOutputPurpose]store.CameraOutputVerification, 3)
	for _, output := range cameraRow.Outputs {
		verification := output.Verification
		verification.CheckedAt = time.Now().UTC()
		localURL := "rtsp://127.0.0.1:8554/" + url.PathEscape(output.StreamName)
		result, probeErr := d.prober.Probe(ctx, localURL, 8*time.Second)
		if !result.CheckedAt.IsZero() {
			verification.CheckedAt = result.CheckedAt
		}
		verification.VideoCodec, verification.AudioCodec, verification.Width, verification.Height, verification.FPS = "", "", 0, 0, 0
		if probeErr != nil {
			verification.Error = "stream output verification failed"
		} else {
			verification.Error = ""
			for _, media := range result.Streams {
				if media.Type == "video" && verification.VideoCodec == "" {
					verification.VideoCodec, verification.Width, verification.Height, verification.FPS = media.Codec, media.Width, media.Height, media.FPS
				}
				if media.Type == "audio" && verification.AudioCodec == "" {
					verification.AudioCodec = media.Codec
				}
			}
		}
		verifications[output.Purpose] = verification
	}
	return d.db.UpdateCameraOutputVerifications(ctx, cameraRow.ID, cameraRow.PolicyState.AppliedRevision, verifications)
}
