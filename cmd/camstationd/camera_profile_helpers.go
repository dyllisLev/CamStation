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

type cameraProfileScanner interface {
	Scan(context.Context, cameraprofile.ScanRequest) (cameraprofile.DeviceProfile, error)
}

var newCameraProfileScanner = func() cameraProfileScanner {
	return cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient())
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
	StreamOutputs       []publicStreamOutputSettings    `json:"streamOutputs"`
	ExpectedRevision    *int64                          `json:"expectedDesiredRevision"`
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
	} else if req.Stream != streamName(req.Stream, 1) {
		return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: invalid stream name", errBadCameraProfileRequest)
	}
	if err := validatePublicOutputSourceKeys(req.StreamOutputs); err != nil {
		return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: %v", errBadCameraProfileRequest, err)
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
	if !hasProfileCandidateURLs(profile) && len(req.Streams) == 0 && scanReqHasTarget(scanReq) {
		scanned, err := newCameraProfileScanner().Scan(ctx, scanReq)
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
	var existing store.Camera
	if stableStreamName != "" {
		existing, _ = db.GetCameraByStream(ctx, stableStreamName)
	}
	streams := toStoreStreams(req.Stream, candidates, "unknown")
	if len(streams) == 0 && existing.ID != 0 {
		streams = existing.Streams
	}
	if len(streams) == 0 {
		streams = []store.CameraStream{{SourceKey: "recording", Role: store.CameraStreamRoleRecording, Label: "recording", URL: primaryURL, Go2RTCStreamName: req.Stream + "-input", State: "unknown"}}
	}
	for _, input := range streams {
		if err := validateStoredProbeTarget(ctx, input.URL); err != nil {
			return store.Camera{}, camera.ProbeResult{}, nil, fmt.Errorf("%w: unsafe resolved stream target", errBadCameraProfileRequest)
		}
	}
	result, probeErr := prober.Probe(ctx, primaryURL, 12*time.Second)
	state := "streaming"
	if probeErr != nil {
		state = "offline"
	}
	for i := range streams {
		streams[i].State = state
		var inputResult camera.ProbeResult
		var inputErr error
		if streams[i].URL == primaryURL {
			inputResult, inputErr = result, probeErr
		} else {
			inputResult, inputErr = prober.Probe(ctx, streams[i].URL, 12*time.Second)
		}
		applyProbeResultToInput(&streams[i], inputResult, inputErr)
	}
	outputs := requestedCameraOutputs(req.StreamOutputs, streams)
	if len(req.StreamOutputs) == 0 && existing.ID != 0 {
		outputs = existing.Outputs
	}
	cameraRow := store.Camera{
		ID:                existing.ID,
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
	}
	if existing.ID != 0 {
		cameraRow.LayoutKey = existing.LayoutKey
		cameraRow.ControlCapabilities = existing.ControlCapabilities
	}
	if len(profile.Channels) > 0 {
		cameraRow.ControlCapabilities = controlCapabilitiesFromProfile(profile)
	}
	cameraRow.Streams, cameraRow.Outputs = streams, outputs
	expected := req.ExpectedRevision
	if existing.ID != 0 && expected == nil {
		value := existing.PolicyState.DesiredRevision
		expected = &value
	}
	saved, err := db.SaveCameraConfiguration(ctx, cameraRow, expected)
	if err != nil {
		return store.Camera{}, camera.ProbeResult{}, nil, err
	}
	return saved, result, probeErr, nil
}

func requestedCameraOutputs(requested []publicStreamOutputSettings, inputs []store.CameraStream) []store.CameraOutput {
	if len(requested) > 0 {
		outputs := make([]store.CameraOutput, 0, len(requested))
		for _, output := range requested {
			outputs = append(outputs, store.CameraOutput{Purpose: output.Purpose, SourceKey: output.SourceKey, VideoMode: output.VideoMode, MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS, AudioMode: output.AudioMode, Activation: output.Activation})
		}
		return outputs
	}
	recording, live := inputs[0].SourceKey, inputs[0].SourceKey
	for _, input := range inputs {
		if input.SourceKey == "recording" {
			recording = input.SourceKey
		}
		if input.SourceKey == "live" {
			live = input.SourceKey
		}
	}
	return []store.CameraOutput{
		{Purpose: store.CameraOutputRecording, SourceKey: recording, VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
		{Purpose: store.CameraOutputLive, SourceKey: live, VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		{Purpose: store.CameraOutputFocus, SourceKey: recording, VideoMode: store.CameraVideoAuto, MaxWidth: intValue(1920), MaxHeight: intValue(1080), AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
	}
}

func applyProbeResultToInput(input *store.CameraStream, result camera.ProbeResult, probeErr error) {
	input.DetectedCheckedAt = result.CheckedAt
	if input.DetectedCheckedAt.IsZero() {
		input.DetectedCheckedAt = time.Now().UTC()
	}
	if probeErr != nil {
		input.DetectedError = store.RedactText(camera.RedactText(probeErr.Error(), input.URL))
		return
	}
	for _, media := range result.Streams {
		if media.Type == "video" && input.DetectedVideoCodec == "" {
			input.DetectedVideoCodec, input.DetectedProfile, input.DetectedLevel = media.Codec, media.Profile, media.Level
			input.DetectedPixelFormat, input.DetectedBitDepth = media.PixelFormat, media.BitDepth
			input.DetectedWidth, input.DetectedHeight, input.DetectedFPS = media.Width, media.Height, media.FPS
		}
		if media.Type == "audio" && input.DetectedAudioCodec == "" {
			input.DetectedAudioCodec = media.Codec
		}
	}
}

func intValue(value int) *int { return &value }

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
