package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"camstation/internal/camera"
	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

type cameraStreamSelection struct {
	Role         cameraprofile.StreamRole `json:"role"`
	ProfileToken string                   `json:"profileToken"`
}

type cameraCreateRequest struct {
	Name                string                          `json:"name"`
	URL                 string                          `json:"url"`
	Stream              string                          `json:"streamName"`
	Host                string                          `json:"host"`
	Username            string                          `json:"username"`
	Password            string                          `json:"password"`
	RTSPPort            int                             `json:"rtspPort"`
	HTTPPort            int                             `json:"httpPort"`
	ONVIFPort           int                             `json:"onvifPort"`
	Adapter             string                          `json:"adapter"`
	ChannelIndex        *int                            `json:"channelIndex"`
	ProfileTemplateID   *int64                          `json:"profileTemplateId"`
	SaveProfileTemplate *cameraProfileTemplateRequest   `json:"saveProfileTemplate"`
	Profile             cameraprofile.DeviceProfile     `json:"profile"`
	Streams             []cameraprofile.StreamCandidate `json:"streams"`
	StreamSelections    []cameraStreamSelection         `json:"streamSelections"`
}

var (
	errBadCameraProfileRequest = errors.New("bad camera profile request")
	errCameraProfileScanFailed = errors.New("camera profile scan failed")
)

func (r cameraCreateRequest) ChannelIndexValue() int {
	if r.ChannelIndex == nil {
		return 0
	}
	return *r.ChannelIndex
}

func cameraUpdateRequest(existing store.Camera, req cameraCreateRequest) cameraCreateRequest {
	req.Stream = existing.StreamName
	if req.Name == "" {
		req.Name = existing.Name
	}
	if req.URL == "" {
		req.URL = existing.URL
	}
	if req.Host == "" {
		req.Host = existing.Host
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = existing.RTSPPort
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = existing.HTTPPort
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = existing.ONVIFPort
	}
	if req.Adapter == "" {
		req.Adapter = existing.ProfileAdapter
	}
	if req.ChannelIndex == nil {
		req.ChannelIndex = existing.ChannelIndex
	}
	if req.ProfileTemplateID == nil {
		req.ProfileTemplateID = existing.ProfileTemplateID
	}
	return req
}

func scanRequestFromCamera(req cameraCreateRequest) cameraprofile.ScanRequest {
	return cameraprofile.ScanRequest{
		Name:      req.Name,
		URL:       req.URL,
		Host:      req.Host,
		Username:  req.Username,
		Password:  req.Password,
		RTSPPort:  req.RTSPPort,
		HTTPPort:  req.HTTPPort,
		ONVIFPort: req.ONVIFPort,
		Adapter:   req.Adapter,
	}
}

func persistCameraProfile(ctx context.Context, db *store.DB, prober camera.Prober, req cameraCreateRequest, stableStreamName string) (store.Camera, camera.ProbeResult, error, error) {
	if req.Name == "" {
		req.Name = "Camera 1"
	}
	if stableStreamName != "" {
		req.Stream = stableStreamName
	}
	if req.Stream == "" {
		req.Stream = streamName(req.Name, 1)
	}

	profile := req.Profile
	if req.SaveProfileTemplate != nil {
		created, err := db.CreateCameraProfileTemplate(ctx, req.SaveProfileTemplate.storeTemplate())
		if err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, err
		}
		req.ProfileTemplateID = &created.ID
	}
	var selectedTemplate *store.CameraProfileTemplate
	if req.ProfileTemplateID != nil {
		template, err := db.GetCameraProfileTemplate(ctx, *req.ProfileTemplateID)
		if err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, err
		}
		selectedTemplate = &template
		profile = mergeTemplateIntoDeviceProfile(profile, template, req)
	}
	scanReq := scanRequestFromCamera(req)
	if !hasProfileCandidates(profile) && len(req.Streams) == 0 && scanReqHasTarget(scanReq) {
		scanned, err := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient()).Scan(ctx, scanReq)
		if err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: %v", errCameraProfileScanFailed, err)
		}
		profile = scanned
	}

	candidates := profileCandidates(profile)
	selectionProfile := profile
	if len(req.Streams) > 0 {
		candidates = req.Streams
		selectionProfile = profileWithCandidates(cameraprofile.DeviceProfile{}, candidates)
	}
	if selectedTemplate != nil && len(candidates) == 0 {
		candidates = profileTemplateCandidates(*selectedTemplate, req)
		selectionProfile = profileWithCandidates(cameraprofile.DeviceProfile{}, candidates)
	}
	if len(req.StreamSelections) > 0 {
		candidates = selectProfileCandidates(selectionProfile, req.ChannelIndexValue(), req.StreamSelections)
		if len(candidates) == 0 {
			return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: selected stream profiles were not found", errBadCameraProfileRequest)
		}
	}
	primaryURL := req.URL
	if primaryURL == "" {
		primaryURL = primaryCandidateURL(candidates)
	}
	if primaryURL == "" {
		return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: url or stream candidates are required", errBadCameraProfileRequest)
	}
	if prober == nil {
		return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("camera prober unavailable")
	}

	result, probeErr := prober.Probe(ctx, primaryURL, 12*time.Second)
	state := "streaming"
	if probeErr != nil {
		state = "offline"
	}

	saved, err := db.UpsertCamera(ctx, store.Camera{
		Name:              req.Name,
		URL:               primaryURL,
		StreamName:        req.Stream,
		State:             state,
		ProfileTemplateID: req.ProfileTemplateID,
		Manufacturer:      profile.Manufacturer,
		Model:             profile.Model,
		ProfileAdapter:    profile.Adapter,
		Host:              firstNonEmpty(profile.Host, scanReq.Host),
		RTSPPort:          firstNonZero(profile.RTSPPort, scanReq.RTSPPort),
		HTTPPort:          firstNonZero(profile.HTTPPort, scanReq.HTTPPort),
		ONVIFPort:         firstNonZero(profile.ONVIFPort, scanReq.ONVIFPort),
		ChannelIndex:      req.ChannelIndex,
		LastProbeJSON:     toMap(result),
		LastScanJSON:      profile.LastScan,
	})
	if err != nil {
		return store.Camera{}, camera.ProbeResult{}, nil, err
	}
	if len(candidates) > 0 {
		if err := db.ReplaceCameraStreams(ctx, saved.ID, toStoreStreams(saved.StreamName, candidates, state)); err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, err
		}
		saved, err = db.GetCameraByStream(ctx, saved.StreamName)
		if err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, err
		}
	}
	return saved, result, probeErr, nil
}

func cameraMutationEvent(successMessage string, probeErr error) (string, string) {
	if probeErr != nil {
		return "error", successMessage + " but probe failed"
	}
	return "info", successMessage
}

type cameraPreviewRequest struct {
	cameraprofile.ScanRequest
	ChannelIndex *int                     `json:"channelIndex"`
	ProfileToken string                   `json:"profileToken"`
	Role         cameraprofile.StreamRole `json:"role"`
}

func (r cameraPreviewRequest) ChannelIndexValue() int {
	if r.ChannelIndex == nil {
		return 0
	}
	return *r.ChannelIndex
}

func previewRequestWithExisting(existing store.Camera, req cameraPreviewRequest) cameraPreviewRequest {
	if req.Name == "" {
		req.Name = existing.Name
	}
	if req.URL == "" {
		req.URL = existing.URL
	}
	if req.Host == "" {
		req.Host = existing.Host
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = existing.RTSPPort
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = existing.HTTPPort
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = existing.ONVIFPort
	}
	if req.Adapter == "" {
		req.Adapter = existing.ProfileAdapter
	}
	if req.ChannelIndex == nil {
		req.ChannelIndex = existing.ChannelIndex
	}
	return req
}
