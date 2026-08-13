package main

import (
	"testing"

	"camstation/internal/store"
	"camstation/internal/stream"
)

func TestRequestedCameraOutputsUseAlwaysHotBrowserLiveProfile(t *testing.T) {
	inputs := []store.CameraStream{
		{SourceKey: "recording"},
		{SourceKey: "live"},
	}
	outputs := requestedCameraOutputs(nil, inputs)
	if len(outputs) != 3 {
		t.Fatalf("outputs = %d", len(outputs))
	}
	live := outputs[1]
	if live.Purpose != store.CameraOutputLive || live.SourceKey != "live" || live.VideoMode != store.CameraVideoH264 ||
		live.MaxWidth == nil || *live.MaxWidth != 1280 || live.MaxHeight == nil || *live.MaxHeight != 720 ||
		live.MaxFPS == nil || *live.MaxFPS != 15 || live.AudioMode != store.CameraAudioNone ||
		live.Activation != store.CameraActivationAlways {
		t.Fatalf("live default = %#v", live)
	}
}

func TestPublicStreamStatusExposesMediaReadiness(t *testing.T) {
	status := publicGo2RTCStatus(stream.Status{
		Installed:           true,
		Running:             true,
		MediaReady:          true,
		ExpectedLiveStreams: 8,
		ReadyLiveStreams:    8,
	})
	if !status.MediaReady || status.ExpectedLiveStreams != 8 || status.ReadyLiveStreams != 8 {
		t.Fatalf("public status = %#v", status)
	}
}
