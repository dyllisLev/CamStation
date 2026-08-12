package main

import (
	"encoding/json"
	"testing"

	"camstation/internal/recorder"
)

func TestPublicRecorderStatusHidesWorkerLastErrorInternals_whenWorkerHasFailureDetails(t *testing.T) {
	t.Parallel()

	// Given
	forbidden := publicHygieneForbidden{
		recordingsDir: "/placeholder/runtime/recordings",
		tempDir:       "/placeholder/runtime/temp",
	}
	internalLastError := "open rtsp://127.0.0.1:8554/front-main failed for /placeholder/runtime/temp/front-main/segment.mp4"
	status := recorder.Status{
		Enabled:        true,
		RecordingsDir:  forbidden.recordingsDir,
		TempDir:        forbidden.tempDir,
		SegmentMinutes: 5,
		Workers: []recorder.WorkerStatus{{
			StreamName: "front-main",
			CameraID:   7,
			State:      "running",
			Input:      "rtsp://127.0.0.1:8554/front-main",
			Current:    "/placeholder/runtime/temp/front-main/segment.mp4",
			LastError:  internalLastError,
		}},
	}

	// When
	publicStatus := publicRecorderStatusFromInternal(status)
	body := publicRecorderStatusBody(t, publicStatus)

	// Then
	assertPublicHygieneCounts(t, "status-last-error", body, forbidden)
	if len(publicStatus.Workers) != 1 {
		t.Fatalf("worker count = %d, want 1", len(publicStatus.Workers))
	}
	if publicStatus.Workers[0].LastError == "" {
		t.Fatal("worker lastError is empty, want safe public error signal")
	}
	if publicStatus.Workers[0].LastError == internalLastError {
		t.Fatal("worker lastError exposes internal text, want safe public error signal")
	}
}

func publicRecorderStatusBody(t *testing.T, status publicRecorderStatus) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal public recorder status: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("unmarshal public recorder status: %v", err)
	}
	return body
}
