package store

import (
	"strings"
	"testing"
	"time"

	"camstation/internal/recordingmedia"
)

func TestPlaybackCatalogueIncludesArchivedCameraAndRetainsDisplayName(t *testing.T) {
	db := newSettingsJobTestDB(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{Name: "창고", StreamName: "warehouse", URL: "rtsp://secret:password@camera/private"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, err = db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: camera.ID, StreamName: "warehouse-record", Filename: "archive.mp4", TSStart: float64(100 + i), Status: "ready"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.DeleteCamera(t.Context(), camera.StreamName); err != nil {
		t.Fatal(err)
	}
	cameras, err := db.ListPlaybackCameras(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 1 || cameras[0].Registered || !cameras[0].HasRecordings || cameras[0].Name != "창고" || !strings.HasPrefix(cameras[0].CameraKey, "camera:") {
		t.Fatalf("catalogue: %+v", cameras)
	}
	if id, err := db.PlaybackCameraID(t.Context(), cameras[0].CameraKey); err != nil || id != camera.ID {
		t.Fatalf("archive key: %d %v", id, err)
	}
}

func TestPlaybackOverlapsHalfOpenRangeAndUniqueOpenPreservesEarlierFile(t *testing.T) {
	db := newSettingsJobTestDB(t)
	end := float64(150)
	original := RecordingSegment{CameraID: 7, StreamName: "archive", Filename: "original.mp4", TSStart: 90, TSEnd: &end, Status: "ready"}
	segment, err := db.OpenRecordingSegmentUnique(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.Filename = "replacement.mp4"
	if _, err := db.OpenRecordingSegmentUnique(t.Context(), replacement); err == nil {
		t.Fatal("identity collision replaced earlier recording")
	}
	saved, err := db.GetRecordingSegmentByID(t.Context(), segment.ID)
	if err != nil || saved.Filename != original.Filename {
		t.Fatalf("original changed: %+v %v", saved, err)
	}
	spans, err := db.PlaybackSpans(t.Context(), 7, 100000, 200000)
	if err != nil || len(spans) != 1 {
		t.Fatalf("overlap: %+v %v", spans, err)
	}
	spans, err = db.PlaybackSpans(t.Context(), 7, 150000, 200000)
	if err != nil || len(spans) != 0 {
		t.Fatalf("exclusive end: %+v %v", spans, err)
	}
	legacy, err := db.ListRecordingSegments(t.Context(), "archive", time.Unix(100, 0), time.Unix(200, 0), "ready")
	if err != nil || len(legacy) != 1 {
		t.Fatalf("legacy overlap: %+v %v", legacy, err)
	}
}

func TestPlaybackPublishedPrefixIsImmutableAndBackupStateIsPreserved(t *testing.T) {
	db := newSettingsJobTestDB(t)
	segment, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: 7, StreamName: "archive", Filename: "fragmented.mp4", TSStart: 100, Status: "recording"})
	if err != nil {
		t.Fatal(err)
	}
	index := recordingmedia.Index{InitLength: 10, CommittedOffset: 30, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{{Sequence: 1, Offset: 10, Length: 20, MediaStartMs: 200, MediaEndMs: 2200, StartMs: 100000, EndMs: 102000}}}
	if err := db.PublishRecordingMedia(t.Context(), segment.ID, index); err != nil {
		t.Fatal(err)
	}
	invalid := index
	invalid.Fragments = append([]recordingmedia.Fragment(nil), index.Fragments...)
	invalid.Fragments[0].Length = 19
	if err := db.PublishRecordingMedia(t.Context(), segment.ID, invalid); err == nil {
		t.Fatal("published byte range changed")
	}
	got, idx, err := db.RecordingMedia(t.Context(), segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BackupState != "pending" || got.Status != "recording" || len(idx.Fragments) != 1 || idx.Fragments[0].Length != 20 {
		t.Fatalf("publication changed canonical state: %+v %+v", got, idx)
	}
	spans, err := db.PlaybackSpans(t.Context(), 7, 101000, 110000)
	if err != nil || len(spans) != 1 || spans[0].EndMs != 102000 {
		t.Fatalf("committed span: %+v %v", spans, err)
	}
}
