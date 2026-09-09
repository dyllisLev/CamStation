package main

import (
	"fmt"
	"strings"
	"time"

	"camstation/internal/store"
	"camstation/internal/stream"
)

type publicCamera struct {
	Name                string                          `json:"name"`
	StreamName          string                          `json:"streamName"`
	LayoutKey           string                          `json:"layoutKey,omitempty"`
	RecordingStreamName string                          `json:"recordingStreamName,omitempty"`
	LiveStreamName      string                          `json:"liveStreamName,omitempty"`
	FocusStreamName     string                          `json:"focusStreamName,omitempty"`
	State               string                          `json:"state"`
	Enabled             bool                            `json:"enabled"`
	ProfileTemplateID   *int64                          `json:"profileTemplateId,omitempty"`
	Manufacturer        string                          `json:"manufacturer,omitempty"`
	Model               string                          `json:"model,omitempty"`
	ProfileAdapter      string                          `json:"profileAdapter,omitempty"`
	Host                string                          `json:"host,omitempty"`
	RTSPPort            int                             `json:"rtspPort,omitempty"`
	HTTPPort            int                             `json:"httpPort,omitempty"`
	ONVIFPort           int                             `json:"onvifPort,omitempty"`
	ChannelIndex        *int                            `json:"channelIndex,omitempty"`
	LastProbeJSON       map[string]any                  `json:"lastProbe,omitempty"`
	LastScanJSON        map[string]any                  `json:"lastScan,omitempty"`
	ControlCapabilities store.CameraControlCapabilities `json:"controlCapabilities"`
	Streams             []publicCameraStream            `json:"streams,omitempty"`
	StreamOutputs       []publicCameraStreamOutput      `json:"streamOutputs"`
	StreamApplyState    publicCameraStreamApplyState    `json:"streamApplyState"`
	CreatedAt           time.Time                       `json:"createdAt"`
	UpdatedAt           time.Time                       `json:"updatedAt"`
}

type publicCameraStream struct {
	SourceKey  string                 `json:"sourceKey"`
	Role       store.CameraStreamRole `json:"role"`
	Label      string                 `json:"label"`
	Advertised *publicMediaDescriptor `json:"advertised"`
	Detected   *publicMediaDescriptor `json:"detected"`
	CheckedAt  string                 `json:"checkedAt,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

type publicMediaDescriptor struct {
	VideoCodec  string  `json:"videoCodec,omitempty"`
	AudioCodec  string  `json:"audioCodec,omitempty"`
	Profile     string  `json:"profile,omitempty"`
	Level       string  `json:"level,omitempty"`
	PixelFormat string  `json:"pixelFormat,omitempty"`
	BitDepth    int     `json:"bitDepth,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	FPS         float64 `json:"fps,omitempty"`
}

type publicEffectiveDescriptor struct {
	VideoCodec  string  `json:"videoCodec,omitempty"`
	AudioCodec  string  `json:"audioCodec,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	FPS         float64 `json:"fps,omitempty"`
	Transcoding bool    `json:"transcoding"`
}

type publicStreamOutputSettings struct {
	Purpose      store.CameraOutputPurpose `json:"purpose"`
	SourceKey    string                    `json:"sourceKey"`
	VideoEncoder store.CameraVideoEncoder  `json:"videoEncoder"`
	VideoMode    store.CameraVideoMode     `json:"videoMode"`
	MaxWidth     *int                      `json:"maxWidth"`
	MaxHeight    *int                      `json:"maxHeight"`
	MaxFPS       *float64                  `json:"maxFPS"`
	AudioMode    store.CameraAudioMode     `json:"audioMode"`
	Activation   store.CameraActivation    `json:"activation"`
}

type publicStreamOutputSource struct {
	Label      string                 `json:"label"`
	Advertised *publicMediaDescriptor `json:"advertised"`
	Detected   *publicMediaDescriptor `json:"detected"`
	CheckedAt  string                 `json:"checkedAt,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

type publicStreamOutputVerification struct {
	State     string `json:"state"`
	CheckedAt string `json:"checkedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

type publicOutputEncoder struct {
	RequestedEncoder string `json:"requestedEncoder"`
	AllocatedEncoder string `json:"allocatedEncoder,omitempty"`
	ActualEncoder    string `json:"actualEncoder,omitempty"`
	State            string `json:"state"`
	Reason           string `json:"reason,omitempty"`
	CheckedAt        string `json:"checkedAt,omitempty"`
}

type publicStreamOutputRuntime struct {
	State         string `json:"state"`
	ProducerCount int    `json:"producerCount"`
	ConsumerCount int    `json:"consumerCount"`
	ViewerCount   int    `json:"viewerCount"`
}

type publicCameraStreamOutput struct {
	Encoder      publicOutputEncoder            `json:"encoder"`
	Purpose      store.CameraOutputPurpose      `json:"purpose"`
	SourceKey    string                         `json:"sourceKey"`
	StreamName   string                         `json:"streamName"`
	Desired      publicStreamOutputSettings     `json:"desired"`
	Applied      *publicStreamOutputSettings    `json:"applied"`
	Source       publicStreamOutputSource       `json:"source"`
	Effective    *publicEffectiveDescriptor     `json:"effective"`
	Verification publicStreamOutputVerification `json:"verification"`
	Runtime      publicStreamOutputRuntime      `json:"runtime"`
}

type publicCameraStreamApplyState struct {
	DesiredRevision int64                  `json:"desiredRevision"`
	AppliedRevision int64                  `json:"appliedRevision"`
	State           store.CameraApplyState `json:"state"`
	AppliedAt       string                 `json:"appliedAt,omitempty"`
	Error           string                 `json:"error,omitempty"`
}

func publicCameras(cameras []store.Camera, statuses ...stream.Status) []publicCamera {
	out := make([]publicCamera, 0, len(cameras))
	for _, camera := range cameras {
		out = append(out, publicCameraFromStore(camera, statuses...))
	}
	return out
}

func publicCameraFromStore(camera store.Camera, statuses ...stream.Status) publicCamera {
	streams := make([]publicCameraStream, 0, len(camera.Streams))
	bySourceKey := make(map[string]store.CameraStream, len(camera.Streams))
	for _, input := range camera.Streams {
		if !isPublicCameraSourceKey(input.SourceKey) {
			continue
		}
		bySourceKey[input.SourceKey] = input
		streams = append(streams, publicCameraStream{
			SourceKey: input.SourceKey, Role: input.Role, Label: input.Label,
			Advertised: advertisedDescriptor(input), Detected: detectedDescriptor(input),
			CheckedAt: formatPublicTime(input.DetectedCheckedAt), Error: publicPolicyError(input.DetectedError),
		})
	}
	var status stream.Status
	if len(statuses) > 0 {
		status = statuses[0]
	}
	outputs := make([]publicCameraStreamOutput, 0, len(camera.Outputs))
	for _, output := range camera.Outputs {
		sourceKey := canonicalPublicSourceKey(output.SourceKey, output.Purpose, bySourceKey)
		input := bySourceKey[sourceKey]
		desired := publicSettings(output.Purpose, sourceKey, output.VideoMode, output.VideoEncoder, output.MaxWidth, output.MaxHeight, output.MaxFPS, output.AudioMode, output.Activation)
		var applied *publicStreamOutputSettings
		if camera.PolicyState.AppliedRevision > 0 && output.AppliedPolicy.SourceKey != "" {
			appliedSourceKey := canonicalPublicSourceKey(output.AppliedPolicy.SourceKey, output.Purpose, bySourceKey)
			value := publicSettings(output.Purpose, appliedSourceKey, output.AppliedPolicy.VideoMode, output.AppliedPolicy.VideoEncoder, output.AppliedPolicy.MaxWidth, output.AppliedPolicy.MaxHeight, output.AppliedPolicy.MaxFPS, output.AppliedPolicy.AudioMode, output.AppliedPolicy.Activation)
			applied = &value
		}
		verificationState := "unverified"
		if output.Verification.Error != "" {
			verificationState = "degraded"
		} else if !output.Verification.CheckedAt.IsZero() {
			verificationState = "healthy"
		}
		var effective *publicEffectiveDescriptor
		if output.Verification.VideoCodec != "" || output.Verification.AudioCodec != "" || output.Verification.Width > 0 {
			effective = &publicEffectiveDescriptor{VideoCodec: output.Verification.VideoCodec, AudioCodec: output.Verification.AudioCodec, Width: output.Verification.Width, Height: output.Verification.Height, FPS: output.Verification.FPS, Transcoding: output.Verification.Transcoding}
		}
		runtime := status.Streams[output.StreamName]
		outputs = append(outputs, publicCameraStreamOutput{
			Purpose: output.Purpose, SourceKey: sourceKey, StreamName: output.StreamName, Desired: desired, Applied: applied,
			Encoder:      publicEncoderStatus(status.EncoderRuntime[fmt.Sprintf("%d_%s", camera.ID, output.Purpose)], applied),
			Source:       publicStreamOutputSource{Label: input.Label, Advertised: advertisedDescriptor(input), Detected: detectedDescriptor(input), CheckedAt: formatPublicTime(input.DetectedCheckedAt), Error: publicPolicyError(input.DetectedError)},
			Effective:    effective,
			Verification: publicStreamOutputVerification{State: verificationState, CheckedAt: formatPublicTime(output.Verification.CheckedAt), Error: publicPolicyError(output.Verification.Error)},
			Runtime:      publicStreamOutputRuntime{State: defaultRuntimeState(runtime.State), ProducerCount: runtime.ProducerCount, ConsumerCount: runtime.ConsumerCount, ViewerCount: runtime.ViewerCount},
		})
	}
	applyState := publicCameraStreamApplyState{DesiredRevision: camera.PolicyState.DesiredRevision, AppliedRevision: camera.PolicyState.AppliedRevision, State: camera.PolicyState.ApplyState, Error: publicPolicyError(camera.PolicyState.ApplyError)}
	if camera.PolicyState.AppliedRevision > 0 {
		applyState.AppliedAt = formatPublicTime(camera.PolicyState.AppliedAt)
	}
	return publicCamera{
		Name:                camera.Name,
		StreamName:          camera.StreamName,
		LayoutKey:           camera.LayoutKey,
		RecordingStreamName: camera.RecordingStreamName,
		LiveStreamName:      camera.LiveStreamName,
		FocusStreamName:     camera.FocusStreamName,
		State:               camera.State,
		Enabled:             camera.Enabled,
		ProfileTemplateID:   camera.ProfileTemplateID,
		Manufacturer:        camera.Manufacturer,
		Model:               camera.Model,
		ProfileAdapter:      camera.ProfileAdapter,
		Host:                camera.Host,
		RTSPPort:            camera.RTSPPort,
		HTTPPort:            camera.HTTPPort,
		ONVIFPort:           camera.ONVIFPort,
		ChannelIndex:        camera.ChannelIndex,
		LastProbeJSON:       publicJSONMap(camera.LastProbeJSON),
		LastScanJSON:        publicJSONMap(camera.LastScanJSON),
		ControlCapabilities: camera.ControlCapabilities,
		Streams:             streams,
		StreamOutputs:       outputs,
		StreamApplyState:    applyState,
		CreatedAt:           camera.CreatedAt,
		UpdatedAt:           camera.UpdatedAt,
	}
}

func isPublicCameraSourceKey(sourceKey string) bool {
	return sourceKey == "recording" || sourceKey == "live"
}

func canonicalPublicSourceKey(sourceKey string, purpose store.CameraOutputPurpose, inputs map[string]store.CameraStream) string {
	if isPublicCameraSourceKey(sourceKey) {
		if _, ok := inputs[sourceKey]; ok {
			return sourceKey
		}
	}
	if purpose == store.CameraOutputLive {
		if _, ok := inputs["live"]; ok {
			return "live"
		}
	}
	return "recording"
}

func publicPolicyError(value string) string {
	return redactInternalRuntimeText(store.RedactText(value))
}

func publicSettings(purpose store.CameraOutputPurpose, sourceKey string, video store.CameraVideoMode, encoder store.CameraVideoEncoder, maxWidth, maxHeight *int, maxFPS *float64, audio store.CameraAudioMode, activation store.CameraActivation) publicStreamOutputSettings {
	return publicStreamOutputSettings{Purpose: purpose, SourceKey: sourceKey, VideoMode: video, VideoEncoder: store.NormalizeCameraVideoEncoder(encoder), MaxWidth: maxWidth, MaxHeight: maxHeight, MaxFPS: maxFPS, AudioMode: audio, Activation: activation}
}

func advertisedDescriptor(input store.CameraStream) *publicMediaDescriptor {
	if input.Codec == "" && input.Width == 0 && input.Height == 0 && input.FPS == 0 {
		return nil
	}
	return &publicMediaDescriptor{VideoCodec: input.Codec, Width: input.Width, Height: input.Height, FPS: input.FPS}
}

func detectedDescriptor(input store.CameraStream) *publicMediaDescriptor {
	if input.DetectedVideoCodec == "" && input.DetectedAudioCodec == "" && input.DetectedWidth == 0 && input.DetectedHeight == 0 {
		return nil
	}
	return &publicMediaDescriptor{VideoCodec: input.DetectedVideoCodec, AudioCodec: input.DetectedAudioCodec, Profile: input.DetectedProfile, Level: input.DetectedLevel, PixelFormat: input.DetectedPixelFormat, BitDepth: input.DetectedBitDepth, Width: input.DetectedWidth, Height: input.DetectedHeight, FPS: input.DetectedFPS}
}

func formatPublicTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func defaultRuntimeState(value string) string {
	if value == "" {
		return "idle"
	}
	return value
}

func publicJSONMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if strings.EqualFold(key, "url") || isSecretJSONKey(key) {
			continue
		}
		out[key] = publicJSONValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func publicJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return publicJSONMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, publicJSONValue(item))
		}
		return out
	case string:
		return publicPolicyError(typed)
	default:
		return value
	}
}

func isSecretJSONKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
	if compact == "profiletoken" {
		return false
	}
	switch compact {
	case "user", "username", "password", "passwd", "pwd", "token", "accesstoken", "authtoken", "refreshtoken", "apikey", "auth", "authorization", "secret", "clientsecret":
		return true
	default:
		return strings.HasSuffix(compact, "token") || strings.Contains(compact, "password") || strings.Contains(compact, "secret") || strings.Contains(compact, "apikey") || strings.Contains(compact, "authorization")
	}
}

type publicEncoderSummary struct {
	SessionLimit  int    `json:"sessionLimit"`
	AssignedNVENC int    `json:"assignedNVENC"`
	RunningNVENC  int    `json:"runningNVENC"`
	CheckedAt     string `json:"checkedAt,omitempty"`
}

type publicStreamStatus struct {
	Encoders            publicEncoderSummary            `json:"encoders"`
	Installed           bool                            `json:"installed"`
	Running             bool                            `json:"running"`
	MediaReady          bool                            `json:"mediaReady"`
	ExpectedLiveStreams int                             `json:"expectedLiveStreams"`
	ReadyLiveStreams    int                             `json:"readyLiveStreams"`
	Error               string                          `json:"error,omitempty"`
	Streams             map[string]stream.StreamRuntime `json:"streams,omitempty"`
}

func publicGo2RTCStatus(status stream.Status) publicStreamStatus {
	summary := publicEncoderSummary{SessionLimit: 3}
	var checkedAt time.Time
	for _, encoder := range status.EncoderRuntime {
		if encoder.AllocatedEncoder == "nvenc" {
			summary.AssignedNVENC++
		}
		if encoder.State == "running" && encoder.ActualEncoder == "nvenc" {
			summary.RunningNVENC++
		}
		if encoder.CheckedAt.After(checkedAt) {
			checkedAt = encoder.CheckedAt
		}
	}
	summary.CheckedAt = formatPublicTime(checkedAt)
	return publicStreamStatus{
		Encoders:            summary,
		Installed:           status.Installed,
		Running:             status.Running,
		MediaReady:          status.MediaReady,
		ExpectedLiveStreams: status.ExpectedLiveStreams,
		ReadyLiveStreams:    status.ReadyLiveStreams,
		Error:               redactInternalRuntimeText(status.Error),
		Streams:             status.Streams,
	}
}

func redactInternalRuntimeText(value string) string {
	for _, internalHost := range []string{"127.0.0.1:1984", "127.0.0.1:8554", "localhost:1984", "localhost:8554"} {
		value = strings.ReplaceAll(value, "http://"+internalHost, "[internal-go2rtc]")
		value = strings.ReplaceAll(value, "rtsp://"+internalHost, "[internal-go2rtc]")
		value = strings.ReplaceAll(value, internalHost, "[internal-go2rtc]")
	}
	return value
}

func publicEncoderStatus(value stream.EncoderRuntime, applied *publicStreamOutputSettings) publicOutputEncoder {
	result := publicOutputEncoder{State: "unverified"}
	if applied != nil {
		result.RequestedEncoder = string(applied.VideoEncoder)
	}
	for _, encoder := range []string{"cpu", "nvenc", "copy"} {
		if value.RequestedEncoder == encoder {
			result.RequestedEncoder = encoder
		}
		if value.AllocatedEncoder == encoder {
			result.AllocatedEncoder = encoder
		}
		if value.State == "running" && value.ActualEncoder == encoder {
			result.ActualEncoder = encoder
		}
	}
	switch value.State {
	case "running", "stopped", "failed":
		result.State = value.State
	}
	switch value.Reason {
	case "device_unavailable", "permission_denied", "library_unavailable", "api_incompatible", "encoder_failed", "session_limit", "cpu_failed", "supervisor_unavailable", "probe_timeout":
		result.Reason = value.Reason
	}
	result.CheckedAt = formatPublicTime(value.CheckedAt)
	return result
}
