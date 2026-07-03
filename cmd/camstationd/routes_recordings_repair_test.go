package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"camstation/internal/store"
)

type recordingSegmentFixture struct {
	streamName string
	status     string
	name       string
	content    string
	tsStart    float64
	finalFile  bool
}

func (s recordingRouteServer) createSegmentFixture(t *testing.T, fixture recordingSegmentFixture) store.RecordingSegment {
	t.Helper()

	finalPath := filepath.Join(s.recordingsDir, fixture.streamName, fixture.name)
	if fixture.finalFile {
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
			t.Fatalf("create final dir: %v", err)
		}
		if err := os.WriteFile(finalPath, []byte(fixture.content), 0o644); err != nil {
			t.Fatalf("write final segment: %v", err)
		}
	}
	segment := store.RecordingSegment{
		CameraID:   21,
		StreamName: fixture.streamName,
		Filename:   fixture.name,
		TempPath:   filepath.Join(s.tempDir, fixture.name),
		FinalPath:  finalPath,
		TSStart:    fixture.tsStart,
		Status:     fixture.status,
	}
	if fixture.status == "ready" && fixture.finalFile {
		size := int64(len(fixture.content))
		segment.FileSize = &size
	}
	created, err := s.db.OpenRecordingSegment(t.Context(), segment)
	if err != nil {
		t.Fatalf("open segment fixture: %v", err)
	}
	return created
}

func TestRecordingsSegmentsAPI_ListFiltersAndPathFreeDTOs(t *testing.T) {
	t.Parallel()

	// Given
	server := newRecordingRouteServer(t)
	server.createSegmentFixture(t, recordingSegmentFixture{streamName: "front-record", status: "ready", name: "before-range.mp4", content: "before", tsStart: 1_783_010_050, finalFile: true})
	server.createSegmentFixture(t, recordingSegmentFixture{streamName: "front-record", status: "ready", name: "a.mp4", content: "a", tsStart: 1_783_010_100, finalFile: true})
	server.createSegmentFixture(t, recordingSegmentFixture{streamName: "front-record", status: "finalizing", name: "excluded-status.mp4", content: "excluded", tsStart: 1_783_010_200, finalFile: true})
	server.createSegmentFixture(t, recordingSegmentFixture{streamName: "side-record", status: "ready", name: "b.mp4", content: "b", tsStart: 1_783_010_200, finalFile: true})
	server.createSegmentFixture(t, recordingSegmentFixture{streamName: "front-record", status: "recording", name: "c.mp4", content: "c", tsStart: 1_783_010_300, finalFile: true})
	server.createSegmentFixture(t, recordingSegmentFixture{streamName: "front-record", status: "ready", name: "after-range.mp4", content: "after", tsStart: 1_783_010_400, finalFile: true})

	// When
	commaStatus, commaBody := requestJSON(t, server.handler, http.MethodGet, "/api/recordings/segments?stream=front-record&status=ready,recording&from=1783010100&to=1783010301", "")
	repeatedStatus, repeatedBody := requestJSON(t, server.handler, http.MethodGet, "/api/recordings/segments?stream=front-record&status=ready&status=recording&from=1783010100&to=1783010301", "")
	limitStatus, limitBody := requestJSON(t, server.handler, http.MethodGet, "/api/recordings/segments?stream=front-record&status=ready,recording&from=1783010100&to=1783010301&limit=1", "")

	// Then
	if commaStatus != http.StatusOK {
		t.Fatalf("comma status = %d, want %d; body=%#v", commaStatus, http.StatusOK, commaBody)
	}
	commaSegments := recordingSegmentsFromBody(t, commaBody)
	assertRecordingSegmentFilenames(t, commaSegments, []string{"c.mp4", "a.mp4"})
	assertRecordingPayloadHasNoInternalPaths(t, server, commaBody)
	repeated := recordingSegmentsFromBody(t, repeatedBody)
	if repeatedStatus != http.StatusOK {
		t.Fatalf("repeated status/body = %d/%#v", repeatedStatus, repeatedBody)
	}
	assertRecordingSegmentFilenames(t, repeated, []string{"c.mp4", "a.mp4"})
	limited := recordingSegmentsFromBody(t, limitBody)
	if limitStatus != http.StatusOK {
		t.Fatalf("limit status/body = %d/%#v", limitStatus, limitBody)
	}
	assertRecordingSegmentFilenames(t, limited, []string{"c.mp4"})
	writeAPIEvidence(t, "recording-segments-filters.json", map[string]any{"commaStatus": commaStatus, "commaBody": commaBody, "repeatedStatus": repeatedStatus, "repeatedBody": repeatedBody, "limitStatus": limitStatus, "limitBody": limitBody})
}

func TestRecordingsSegmentsAPI_DeleteRejectsUnsafeStatesAndMissingFinalFile(t *testing.T) {
	t.Parallel()

	// Given
	server := newRecordingRouteServer(t)
	cases := []recordingSegmentFixture{
		{streamName: "front-finalizing", status: "finalizing", name: "finalizing.mp4", content: "finalizing", tsStart: 1_783_020_100, finalFile: true},
		{streamName: "front-failed", status: "failed", name: "failed.mp4", content: "failed", tsStart: 1_783_020_200},
		{streamName: "front-deleted", status: "deleted", name: "deleted.mp4", content: "deleted", tsStart: 1_783_020_300, finalFile: true},
		{streamName: "front-temp", status: "recording", name: "temp.mp4", content: "temp", tsStart: 1_783_020_400},
	}

	// When / Then
	results := make([]map[string]any, 0, len(cases)+1)
	for _, fixture := range cases {
		segment := server.createSegmentFixture(t, fixture)
		status, body := requestJSON(t, server.handler, http.MethodDelete, "/api/recordings/segments/"+strconv.FormatInt(segment.ID, 10), "")
		if status != http.StatusConflict {
			t.Fatalf("%s delete status = %d, want %d; body=%#v", fixture.status, status, http.StatusConflict, body)
		}
		if fixture.finalFile {
			assertFileExists(t, segment.FinalPath)
		}
		results = append(results, map[string]any{"case": fixture.status, "status": status, "body": body})
	}
	missing := server.createSegmentFixture(t, recordingSegmentFixture{streamName: "front-missing", status: "ready", name: "missing.mp4", tsStart: 1_783_020_500})
	status, body := requestJSON(t, server.handler, http.MethodDelete, "/api/recordings/segments/"+strconv.FormatInt(missing.ID, 10), "")
	if status != http.StatusNotFound {
		t.Fatalf("missing final file status = %d, want %d; body=%#v", status, http.StatusNotFound, body)
	}
	results = append(results, map[string]any{"case": "missing-final-file", "status": status, "body": body})
	writeAPIEvidence(t, "recording-segments-delete-rejections.json", map[string]any{"results": results})
}

func recordingSegmentsFromBody(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()

	rawSegments, ok := body["segments"].([]any)
	if !ok {
		t.Fatalf("segments missing: %#v", body)
	}
	segments := make([]map[string]any, 0, len(rawSegments))
	for _, raw := range rawSegments {
		segment, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("segment is not an object: %#v", raw)
		}
		segments = append(segments, segment)
	}
	return segments
}

func assertRecordingSegmentFilenames(t *testing.T, segments []map[string]any, want []string) {
	t.Helper()

	if len(segments) != len(want) {
		t.Fatalf("segment count = %d, want %d; segments=%#v", len(segments), len(want), segments)
	}
	for index, filename := range want {
		if segments[index]["filename"] != filename {
			t.Fatalf("segment %d filename = %v, want %s; segments=%#v", index, segments[index]["filename"], filename, segments)
		}
	}
}

func assertRecordingPayloadHasNoInternalPaths(t *testing.T, server recordingRouteServer, payload map[string]any) {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal recording payload: %v", err)
	}
	body := string(encoded)
	for _, forbidden := range []string{"tempPath", "finalPath", server.recordingsDir, server.tempDir} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("recording payload leaked %q: %s", forbidden, body)
		}
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to remain at %s: %v", path, err)
	}
}
