package main

import (
	"strings"
	"time"

	"camstation/internal/store"
	"camstation/internal/stream"
)

type publicCamera struct {
	ID                  int64                `json:"id"`
	Name                string               `json:"name"`
	RedactedURL         string               `json:"redactedUrl"`
	StreamName          string               `json:"streamName"`
	LayoutKey           string               `json:"layoutKey,omitempty"`
	RecordingStreamName string               `json:"recordingStreamName,omitempty"`
	LiveStreamName      string               `json:"liveStreamName,omitempty"`
	State               string               `json:"state"`
	Manufacturer        string               `json:"manufacturer,omitempty"`
	Model               string               `json:"model,omitempty"`
	ProfileAdapter      string               `json:"profileAdapter,omitempty"`
	Host                string               `json:"host,omitempty"`
	RTSPPort            int                  `json:"rtspPort,omitempty"`
	HTTPPort            int                  `json:"httpPort,omitempty"`
	ONVIFPort           int                  `json:"onvifPort,omitempty"`
	ChannelIndex        *int                 `json:"channelIndex,omitempty"`
	LastProbeJSON       map[string]any       `json:"lastProbe,omitempty"`
	LastScanJSON        map[string]any       `json:"lastScan,omitempty"`
	Streams             []publicCameraStream `json:"streams,omitempty"`
	CreatedAt           time.Time            `json:"createdAt"`
	UpdatedAt           time.Time            `json:"updatedAt"`
}

type publicCameraStream struct {
	ID               int64                  `json:"id"`
	CameraID         int64                  `json:"camera_id"`
	Role             store.CameraStreamRole `json:"role"`
	Label            string                 `json:"label"`
	Source           string                 `json:"source"`
	RedactedURL      string                 `json:"redactedUrl"`
	Go2RTCStreamName string                 `json:"go2rtcStreamName"`
	Codec            string                 `json:"codec,omitempty"`
	Width            int                    `json:"width,omitempty"`
	Height           int                    `json:"height,omitempty"`
	FPS              float64                `json:"fps,omitempty"`
	BitrateKbps      int                    `json:"bitrateKbps,omitempty"`
	ProfileToken     string                 `json:"profileToken,omitempty"`
	State            string                 `json:"state,omitempty"`
	CreatedAt        time.Time              `json:"createdAt,omitempty"`
	UpdatedAt        time.Time              `json:"updatedAt,omitempty"`
}

func publicCameras(cameras []store.Camera) []publicCamera {
	out := make([]publicCamera, 0, len(cameras))
	for _, camera := range cameras {
		out = append(out, publicCameraFromStore(camera))
	}
	return out
}

func publicCameraFromStore(camera store.Camera) publicCamera {
	streams := make([]publicCameraStream, 0, len(camera.Streams))
	for _, stream := range camera.Streams {
		streams = append(streams, publicCameraStream{
			ID:               stream.ID,
			CameraID:         stream.CameraID,
			Role:             stream.Role,
			Label:            stream.Label,
			Source:           stream.Source,
			RedactedURL:      stream.RedactedURL,
			Go2RTCStreamName: stream.Go2RTCStreamName,
			Codec:            stream.Codec,
			Width:            stream.Width,
			Height:           stream.Height,
			FPS:              stream.FPS,
			BitrateKbps:      stream.BitrateKbps,
			ProfileToken:     stream.ProfileToken,
			State:            stream.State,
			CreatedAt:        stream.CreatedAt,
			UpdatedAt:        stream.UpdatedAt,
		})
	}
	return publicCamera{
		ID:                  camera.ID,
		Name:                camera.Name,
		RedactedURL:         camera.RedactedURL,
		StreamName:          camera.StreamName,
		LayoutKey:           camera.LayoutKey,
		RecordingStreamName: camera.RecordingStreamName,
		LiveStreamName:      camera.LiveStreamName,
		State:               camera.State,
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
		Streams:             streams,
		CreatedAt:           camera.CreatedAt,
		UpdatedAt:           camera.UpdatedAt,
	}
}

func publicJSONMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if strings.EqualFold(key, "url") {
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
		if strings.Contains(typed, "://") && strings.Contains(typed, "@") {
			return store.RedactURL(typed)
		}
		return typed
	default:
		return value
	}
}

type publicStreamStatus struct {
	Installed bool                            `json:"installed"`
	Running   bool                            `json:"running"`
	Error     string                          `json:"error,omitempty"`
	Streams   map[string]stream.StreamRuntime `json:"streams,omitempty"`
}

func publicGo2RTCStatus(status stream.Status) publicStreamStatus {
	return publicStreamStatus{
		Installed: status.Installed,
		Running:   status.Running,
		Error:     redactInternalRuntimeText(status.Error),
		Streams:   status.Streams,
	}
}

func redactInternalRuntimeText(value string) string {
	internalHost := "127.0.0.1" + ":1984"
	value = strings.ReplaceAll(value, "http://"+internalHost, "[internal-go2rtc]")
	value = strings.ReplaceAll(value, internalHost, "[internal-go2rtc]")
	return value
}
