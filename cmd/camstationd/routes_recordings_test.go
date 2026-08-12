package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type recordingRouteServer struct {
	db            *store.DB
	handler       http.Handler
	recordingsDir string
	tempDir       string
}

func newRecordingRouteServer(t *testing.T) recordingRouteServer {
	t.Helper()

	tempDir := t.TempDir()
	db, err := store.Open(filepath.Join(tempDir, "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	recordingsDir := filepath.Join(tempDir, "recordings")
	tempRecordingDir := filepath.Join(tempDir, "temp")
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		t.Fatalf("create recordings dir: %v", err)
	}
	if err := os.MkdirAll(tempRecordingDir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	handler, err := routes(
		db,
		nil,
		stream.NewGo2RTC(filepath.Join(tempDir, "go2rtc.yaml")),
		recorder.New(db, recordingsDir, tempRecordingDir, 5),
		cleanup.New(db, recordingsDir),
		recordingsDir,
		tempRecordingDir,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}
	return recordingRouteServer{db: db, handler: handler, recordingsDir: recordingsDir, tempDir: tempRecordingDir}
}

func (s recordingRouteServer) createReadySegment(t *testing.T, name string, content string) store.RecordingSegment {
	t.Helper()

	finalPath := filepath.Join(s.recordingsDir, "front", "2026-07-02", name)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("create final dir: %v", err)
	}
	if err := os.WriteFile(finalPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write final segment: %v", err)
	}
	size := int64(len(content))
	opened, err := s.db.OpenRecordingSegment(t.Context(), store.RecordingSegment{
		CameraID:   11,
		StreamName: "front-record",
		Filename:   name,
		TempPath:   filepath.Join(s.tempDir, name),
		TSStart:    1_783_001_000,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if err := s.db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+30, finalPath, &size); err != nil {
		t.Fatalf("close segment: %v", err)
	}
	ready, err := s.db.GetRecordingSegment(t.Context(), opened.StreamName, opened.TSStart)
	if err != nil {
		t.Fatalf("reload ready segment: %v", err)
	}
	return ready
}

func TestRecordingsSegmentsAPI_ListDetailDownloadPlayAndDeleteReadySegment(t *testing.T) {
	t.Parallel()

	// Given
	server := newRecordingRouteServer(t)
	segment := server.createReadySegment(t, "front-ready.mp4", "0123456789")
	segmentPath := "/api/recordings/segments/" + strconv.FormatInt(segment.ID, 10)

	// When
	listStatus, listBody := requestJSON(t, server.handler, http.MethodGet, "/api/recordings/segments", "")
	detailStatus, detailBody := requestJSON(t, server.handler, http.MethodGet, segmentPath, "")
	download := performRecordingRequest(t, server.handler, recordingHTTPRequest{method: http.MethodGet, target: segmentPath + "/download"})
	play := performRecordingRequest(t, server.handler, recordingHTTPRequest{method: http.MethodGet, target: segmentPath + "/play", rangeHeader: "bytes=2-5"})
	deleteStatus, deleteBody := requestJSON(t, server.handler, http.MethodDelete, segmentPath, "")

	// Then
	if listStatus != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%#v", listStatus, http.StatusOK, listBody)
	}
	segments, ok := listBody["segments"].([]any)
	if !ok || len(segments) != 1 {
		t.Fatalf("list segments = %#v, want one segment", listBody["segments"])
	}
	assertRecordingPayloadHasNoInternalPaths(t, server, listBody)
	if detailStatus != http.StatusOK || detailBody["id"] != float64(segment.ID) {
		t.Fatalf("detail status/body = %d/%#v", detailStatus, detailBody)
	}
	assertRecordingPayloadHasNoInternalPaths(t, server, detailBody)
	if detailBody["playUrl"] == "" || detailBody["downloadUrl"] == "" {
		t.Fatalf("detail play/download urls missing: %#v", detailBody)
	}
	if download.status != http.StatusOK || download.body != "0123456789" {
		t.Fatalf("download = %#v, want full file", download)
	}
	if play.status != http.StatusPartialContent || play.body != "2345" {
		t.Fatalf("play range = %#v, want partial file", play)
	}
	if play.header.Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("play Content-Range = %q", play.header.Get("Content-Range"))
	}
	if deleteStatus != http.StatusOK || deleteBody["status"] != "deleted" {
		t.Fatalf("delete status/body = %d/%#v", deleteStatus, deleteBody)
	}
	assertRecordingPayloadHasNoInternalPaths(t, server, deleteBody)
	if _, err := os.Stat(segment.FinalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted final file stat error = %v, want not exist", err)
	}

	writeAPIEvidence(t, "recording-segments-list.json", map[string]any{"status": listStatus, "body": listBody})
	writeAPIEvidence(t, "recording-segments-detail.json", map[string]any{"status": detailStatus, "body": detailBody})
	writeAPIEvidence(t, "recording-segments-download.json", map[string]any{"status": download.status, "contentType": download.header.Get("Content-Type"), "body": download.body})
	writeAPIEvidence(t, "recording-segments-play-range.json", map[string]any{"status": play.status, "contentRange": play.header.Get("Content-Range"), "body": play.body})
	writeAPIEvidence(t, "recording-segments-delete.json", map[string]any{"status": deleteStatus, "body": deleteBody})
}

func TestRecordingsSegmentsAPI_DeleteRejectsRecordingSegmentAndLeavesFile(t *testing.T) {
	t.Parallel()

	// Given
	server := newRecordingRouteServer(t)
	activePath := filepath.Join(server.tempDir, "active.mp4")
	if err := os.WriteFile(activePath, []byte("active"), 0o644); err != nil {
		t.Fatalf("write active file: %v", err)
	}
	active, err := server.db.OpenRecordingSegment(t.Context(), store.RecordingSegment{
		CameraID:   12,
		StreamName: "front-active",
		Filename:   filepath.Base(activePath),
		TempPath:   activePath,
		TSStart:    1_783_001_100,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open active segment: %v", err)
	}

	// When
	status, body := requestJSON(t, server.handler, http.MethodDelete, "/api/recordings/segments/"+strconv.FormatInt(active.ID, 10), "")

	// Then
	if status != http.StatusConflict {
		t.Fatalf("active delete status = %d, want %d; body=%#v", status, http.StatusConflict, body)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active file should remain: %v", err)
	}
	writeAPIEvidence(t, "failure.json", map[string]any{"status": status, "body": body, "fileExists": true})
}

type recordingHTTPResult struct {
	status int
	header http.Header
	body   string
}

type recordingHTTPRequest struct {
	method      string
	target      string
	rangeHeader string
}

func performRecordingRequest(t *testing.T, handler http.Handler, request recordingHTTPRequest) recordingHTTPResult {
	t.Helper()

	req := httptest.NewRequest(request.method, request.target, nil)
	if request.rangeHeader != "" {
		req.Header.Set("Range", request.rangeHeader)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return recordingHTTPResult{status: rec.Code, header: rec.Result().Header, body: rec.Body.String()}
}

func decodeRecordingJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}
