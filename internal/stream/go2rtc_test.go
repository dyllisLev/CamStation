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
