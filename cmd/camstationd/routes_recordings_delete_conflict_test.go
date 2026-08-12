package main

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestRecordingsSegmentsAPI_DeleteReturnsConflictWithoutPath_whenStagedDeleteExists(t *testing.T) {
	t.Parallel()

	// Given
	server := newRecordingRouteServer(t)
	segment := server.createReadySegment(t, "front-delete-collision.mp4", "collision")
	stagedPath := segment.FinalPath + ".deleting-" + strconv.FormatInt(segment.ID, 10)
	if err := os.WriteFile(stagedPath, []byte("staged"), 0o644); err != nil {
		t.Fatalf("write staged delete file: %v", err)
	}

	// When
	status, body := requestJSON(t, server.handler, http.MethodDelete, "/api/recordings/segments/"+strconv.FormatInt(segment.ID, 10), "")

	// Then
	if status != http.StatusConflict {
		t.Fatalf("staged delete status = %d, want %d; body=%#v", status, http.StatusConflict, body)
	}
	encoded := mustMarshalString(t, body)
	for _, forbidden := range []string{stagedPath, server.recordingsDir, ".deleting-"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("staged delete conflict response leaked forbidden path material")
		}
	}
	writeAPIEvidence(t, "recording-segments-delete-conflict.json", map[string]any{
		"status":                 status,
		"containsInternalPath":   false,
		"containsStagedSuffix":   false,
		"mappedDeleteConflict":   true,
		"responseErrorKeyCount":  countJSONKey(body, "error"),
		"responseStatusKeyCount": countJSONKey(body, "status"),
	})
}
