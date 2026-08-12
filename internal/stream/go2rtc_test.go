package stream

import (
	"strings"
	"testing"
)

func TestParseStreamRuntimeCountsViewersSeparatelyFromRecorderConsumers(t *testing.T) {
	t.Parallel()

	raw := `{
		"camera-1": {
			"producers": [{"id": 1, "format_name": "rtsp"}],
			"consumers": [
				{"id": 2, "format_name": "rtsp", "protocol": "rtsp+tcp", "user_agent": "Lavf60.16.100"},
				{"id": 3, "format_name": "mse/fmp4", "protocol": "ws", "user_agent": "Mozilla/5.0"}
			]
		},
		"camera-2": {
			"producers": [],
			"consumers": []
		}
	}`

	runtime, err := parseStreamRuntime(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse stream runtime: %v", err)
	}

	camera1 := runtime["camera-1"]
	if camera1.State != "running" {
		t.Fatalf("camera-1 state = %q, want running", camera1.State)
	}
	if camera1.ProducerCount != 1 {
		t.Fatalf("camera-1 producer count = %d, want 1", camera1.ProducerCount)
	}
	if camera1.ConsumerCount != 2 {
		t.Fatalf("camera-1 consumer count = %d, want 2", camera1.ConsumerCount)
	}
	if camera1.ViewerCount != 1 {
		t.Fatalf("camera-1 viewer count = %d, want 1", camera1.ViewerCount)
	}

	camera2 := runtime["camera-2"]
	if camera2.State != "idle" {
		t.Fatalf("camera-2 state = %q, want idle", camera2.State)
	}
}

func TestParseStreamRuntimeRedactsURLStreamNames(t *testing.T) {
	t.Parallel()

	raw := `{
		"rtsp://admin:secret@192.168.0.55:10554/tcp/av0_1": {
			"producers": [{"id": 1, "format_name": "rtsp"}],
			"consumers": []
		}
	}`

	runtime, err := parseStreamRuntime(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse stream runtime: %v", err)
	}
	for streamName := range runtime {
		if strings.Contains(streamName, "secret") || strings.Contains(streamName, "admin:") {
			t.Fatalf("stream runtime exposed secret in key %q", streamName)
		}
		if !strings.Contains(streamName, "redacted:redacted") {
			t.Fatalf("stream runtime key = %q, want redacted URL", streamName)
		}
	}
}

func TestParseStreamRuntimeOmitsPrivateInputProducers(t *testing.T) {
	raw := `{
		"__camstation_source_12_recording": {"producers": [{"id": 1}], "consumers": []},
		"camera-recording": {"producers": [{"id": 2}], "consumers": []}
	}`
	runtime, err := parseStreamRuntime(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime["__camstation_source_12_recording"]; ok {
		t.Fatalf("private producer exposed: %#v", runtime)
	}
	if _, ok := runtime["camera-recording"]; !ok {
		t.Fatalf("public output missing: %#v", runtime)
	}
}

func TestParseStreamRuntimeDoesNotCountConfiguredProducerPlaceholder(t *testing.T) {
	raw := `{
		"failed-live": {"producers": [{"url": "ffmpeg:private-source"}], "consumers": []},
		"active-live": {"producers": [{"id": 7, "format_name": "rtsp", "protocol": "rtsp+tcp", "medias": ["video, recvonly, H264"]}], "consumers": []}
	}`
	runtime, err := parseStreamRuntime(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime["failed-live"]; got.ProducerCount != 0 || got.State != "idle" {
		t.Fatalf("failed-live runtime = %#v, want zero producers and idle", got)
	}
	if got := runtime["active-live"]; got.ProducerCount != 1 || got.State != "running" {
		t.Fatalf("active-live runtime = %#v, want one producer and running", got)
	}
}
