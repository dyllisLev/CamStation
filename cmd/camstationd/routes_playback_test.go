package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/recordingmedia"
	"camstation/internal/store"
)

func playbackRequest(t *testing.T, s recordingRouteServer, path string) (int, string) {
	t.Helper()
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code, w.Body.String()
}
func playbackJSON(t *testing.T, s recordingRouteServer, path string) map[string]any {
	t.Helper()
	status, body := playbackRequest(t, s, path)
	if status != 200 {
		t.Fatalf("GET %s: %d %s", path, status, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPlaybackLegacyResolveOverlapGapAndPagination(t *testing.T) {
	s := newRecordingRouteServer(t)
	for i := 0; i < 3; i++ {
		start := float64(100 + i*100)
		end := start + 30
		_, err := s.db.OpenRecordingSegmentUnique(t.Context(), store.RecordingSegment{CameraID: 11, StreamName: "legacy", Filename: fmt.Sprintf("%d.mp4", i), TSStart: start, TSEnd: &end, Status: "ready"})
		if err != nil {
			t.Fatal(err)
		}
	}
	timeline := playbackJSON(t, s, "/api/playback/timeline?cameraKey=camera:11&fromMs=110000&toMs=210000")
	if len(timeline["coverage"].([]any)) != 2 {
		t.Fatalf("crossing ranges missing: %+v", timeline)
	}
	found := playbackJSON(t, s, "/api/playback/resolve?cameraKey=camera:11&atMs=110000")
	media := found["media"].(map[string]any)
	if found["status"] != "found" || media["kind"] != "file" || media["requestedOffsetSeconds"] != float64(10) || media["timeBasis"] != "legacy_filename" {
		t.Fatalf("legacy mapping: %+v", found)
	}
	gap := playbackJSON(t, s, "/api/playback/resolve?cameraKey=camera:11&atMs=130000")
	if gap["status"] != "gap" || gap["previousAtMs"] != float64(129999) || gap["nextAtMs"] != float64(200000) {
		t.Fatalf("gap mapping: %+v", gap)
	}
	first := playbackJSON(t, s, "/api/playback/segments?cameraKey=camera:11&fromMs=0&toMs=400000&limit=2")
	cursor := first["nextCursor"].(string)
	end := float64(390)
	if _, err := s.db.OpenRecordingSegmentUnique(t.Context(), store.RecordingSegment{CameraID: 11, StreamName: "legacy", Filename: "inserted.mp4", TSStart: 360, TSEnd: &end, Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	second := playbackJSON(t, s, "/api/playback/segments?cameraKey=camera:11&fromMs=0&toMs=400000&limit=2&cursor="+cursor)
	rows := second["segments"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["startMs"] != float64(100000) || second["nextCursor"] != nil {
		t.Fatalf("stable cursor: %+v", second)
	}
	for _, path := range []string{"/api/playback/timeline?cameraKey=camera:11&fromMs=2&toMs=1", "/api/playback/resolve?cameraKey=camera:11&atMs=NaN", "/api/playback/resolve?cameraKey=unknown&atMs=1"} {
		if status, _ := playbackRequest(t, s, path); status != 400 {
			t.Fatalf("invalid request accepted: %s %d", path, status)
		}
	}
}

func TestPlaybackCommittedBytesSurviveArchiveMoveAndDeleteReturnsGone(t *testing.T) {
	s := newRecordingRouteServer(t)
	tempPath := filepath.Join(s.tempDir, "growing.mp4")
	if err := os.WriteFile(tempPath, []byte("INITFRAGMENTincomplete-secret-tail"), 0600); err != nil {
		t.Fatal(err)
	}
	segment, err := s.db.OpenRecordingSegmentUnique(t.Context(), store.RecordingSegment{CameraID: 12, StreamName: "growing", Filename: "growing.mp4", TempPath: tempPath, TSStart: 100, Status: "recording"})
	if err != nil {
		t.Fatal(err)
	}
	idx := recordingmedia.Index{InitLength: 4, CommittedOffset: 12, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{{Sequence: 1, Offset: 4, Length: 8, MediaStartMs: 0, MediaEndMs: 2000, StartMs: 100000, EndMs: 102000}}}
	if err := s.db.PublishRecordingMedia(t.Context(), segment.ID, idx); err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/api/playback/media/fmp4-%d/", segment.ID)
	if status, body := playbackRequest(t, s, base+"fragments/1"); status != 200 || body != "FRAGMENT" {
		t.Fatalf("committed bytes: %d %q", status, body)
	}
	if status, body := playbackRequest(t, s, base+"init"); status != 200 || body != "INIT" {
		t.Fatalf("init: %d %q", status, body)
	}
	if status, _ := playbackRequest(t, s, base+"fragments/2"); status != 404 {
		t.Fatalf("unpublished fragment: %d", status)
	}
	_, manifest := playbackRequest(t, s, base+"manifest.m3u8")
	if strings.Contains(manifest, "#EXT-X-ENDLIST") || strings.Contains(manifest, s.tempDir) || !strings.Contains(manifest, "fragments/1") {
		t.Fatalf("growing manifest: %s", manifest)
	}
	timeline := playbackJSON(t, s, "/api/playback/timeline?cameraKey=camera:12&fromMs=99000&toMs=110000")
	if timeline["playableEndMs"] != float64(102000) || timeline["recordingActive"] != true {
		t.Fatalf("committed end: %+v", timeline)
	}
	edge := playbackJSON(t, s, "/api/playback/resolve?cameraKey=camera:12&atMs=103000")
	if edge["status"] != "edge_wait" {
		t.Fatalf("edge: %+v", edge)
	}
	finalPath := filepath.Join(s.recordingsDir, "growing.mp4")
	size := int64(12)
	if err := s.db.WithRecordingMediaLock(func() error {
		if err := os.Rename(tempPath, finalPath); err != nil {
			return err
		}
		return s.db.CloseRecordingSegment(t.Context(), segment.StreamName, segment.Filename, 102, finalPath, &size)
	}); err != nil {
		t.Fatal(err)
	}
	if status, body := playbackRequest(t, s, base+"fragments/1"); status != 200 || body != "FRAGMENT" {
		t.Fatalf("after move: %d %q", status, body)
	}
	_, manifest = playbackRequest(t, s, base+"manifest.m3u8")
	if !strings.Contains(manifest, "#EXT-X-ENDLIST") {
		t.Fatalf("closed manifest: %s", manifest)
	}
	if _, err := s.db.DeleteReadyRecordingSegmentFile(t.Context(), segment.ID, s.recordingsDir); err != nil {
		t.Fatal(err)
	}
	if status, body := playbackRequest(t, s, base+"fragments/1"); status != 410 || strings.Contains(body, s.recordingsDir) {
		t.Fatalf("deleted response: %d %s", status, body)
	}
}

func TestPlaybackAudioPrerollPreservesVideoClockAndManifest(t *testing.T) {
	s := newRecordingRouteServer(t)
	segment, err := s.db.OpenRecordingSegmentUnique(t.Context(), store.RecordingSegment{CameraID: 13, StreamName: "preroll", Filename: "preroll.mp4", TSStart: 98, Status: "recording"})
	if err != nil {
		t.Fatal(err)
	}
	idx := recordingmedia.Index{InitLength: 4, CommittedOffset: 20, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{
		{Sequence: 1, Offset: 4, Length: 8, MediaStartMs: 2000, MediaEndMs: 4000, StartMs: 100000, EndMs: 102000},
		{Sequence: 2, Offset: 12, Length: 8, MediaStartMs: 4000, MediaEndMs: 6000, StartMs: 102000, EndMs: 104000},
	}}
	if err := s.db.PublishRecordingMedia(t.Context(), segment.ID, idx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at     int
		offset float64
	}{{100500, 2.5}, {102500, 4.5}} {
		found := playbackJSON(t, s, fmt.Sprintf("/api/playback/resolve?cameraKey=camera:13&atMs=%d", tc.at))
		media := found["media"].(map[string]any)
		if found["status"] != "found" || media["startMs"] != float64(100000) || media["mediaStartSeconds"] != float64(2) || media["requestedOffsetSeconds"] != tc.offset {
			t.Fatalf("video clock lost shared origin: %+v", found)
		}
	}
	gap := playbackJSON(t, s, "/api/playback/resolve?cameraKey=camera:13&atMs=99000")
	if gap["status"] != "gap" || gap["nextAtMs"] != float64(100000) {
		t.Fatalf("audio preroll advertised as video: %+v", gap)
	}
	status, manifest := playbackRequest(t, s, fmt.Sprintf("/api/playback/media/fmp4-%d/manifest.m3u8", segment.ID))
	if status != 200 {
		t.Fatalf("manifest: %d %s", status, manifest)
	}
	for _, expected := range []string{"#EXT-X-TARGETDURATION:4\n", "#EXT-X-PROGRAM-DATE-TIME:1970-01-01T00:01:38.000Z\n#EXTINF:4.000,\nfragments/1", "#EXT-X-PROGRAM-DATE-TIME:1970-01-01T00:01:42.000Z\n#EXTINF:2.000,\nfragments/2"} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("missing %q in %s", expected, manifest)
		}
	}
}
