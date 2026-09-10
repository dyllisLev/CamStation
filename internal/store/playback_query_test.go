package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"camstation/internal/recordingmedia"
)

// Match the archive size that blocked a fresh eight-camera live workspace:
// roughly 11k segments, 200 fragmented files and 120k published fragments.
// Use the application's SQLite driver and migrations, including its single
// connection, so this benchmark measures the actual playback query workload.
func playbackArchiveFixture(t testing.TB) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "playback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<11157)
 INSERT INTO recording_segments(id,camera_id,stream_name,filename,ts_start,ts_end,status,created_at,updated_at)
 SELECT n,1+n%8,'archive-'||(1+n%8),'archive-'||n||'.mp4',1788912000+n*1800,1788912000+(n+1)*1800,'ready',0,0 FROM ids`,
		`INSERT INTO recording_media(segment_id,init_length,committed_offset,video_codec,time_basis)
 SELECT id,10,12010,'avc1','media_epoch' FROM recording_segments WHERE id>10957`,
		`WITH RECURSIVE seq(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM seq WHERE n<599)
 INSERT INTO recording_fragments(segment_id,sequence,byte_offset,byte_length,media_start_ms,media_end_ms,start_ms,end_ms)
 SELECT s.id,n,10+n*20,20,n*3000,(n+1)*3000,CAST(s.ts_start*1000 AS INTEGER)+n*3000,CAST(s.ts_start*1000 AS INTEGER)+(n+1)*3000
 FROM recording_segments s CROSS JOIN seq WHERE s.id>10957`,
	} {
		if _, err := db.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func BenchmarkPlaybackEightCameraTimeline(b *testing.B) {
	db := playbackArchiveFixture(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for camera := int64(1); camera <= 8; camera++ {
			spans, err := db.PlaybackSpans(ctx, camera, 1788912000000+11100*1800000, 1788912000000+11158*1800000)
			if err != nil || len(spans) == 0 {
				b.Fatalf("camera %d spans=%d: %v", camera, len(spans), err)
			}
			end, active, err := db.PlaybackBounds(ctx, camera)
			if err != nil || end == nil || active {
				b.Fatalf("camera %d bounds=%v active=%v: %v", camera, end, active, err)
			}
		}
	}
}

func TestPlaybackQueriesKeepSelectedCameraCompletePublishedSpans(t *testing.T) {
	db := newSettingsJobTestDB(t)
	open := func(camera, start, end int64, status string) RecordingSegment {
		t.Helper()
		s := RecordingSegment{CameraID: camera, StreamName: fmt.Sprintf("camera-%d", camera), Filename: fmt.Sprintf("%d.mp4", start), TSStart: float64(start), Status: status}
		if end > 0 {
			v := float64(end)
			s.TSEnd = &v
		}
		s, err := db.OpenRecordingSegmentUnique(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	legacy := open(7, 90, 150, "ready")
	fragmented := open(7, 200, 999, "recording")
	index := recordingmedia.Index{InitLength: 10, CommittedOffset: 50, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{
		{Sequence: 1, Offset: 10, Length: 20, MediaStartMs: 0, MediaEndMs: 2000, StartMs: 200000, EndMs: 260000},
		{Sequence: 2, Offset: 30, Length: 20, MediaStartMs: 2000, MediaEndMs: 4000, StartMs: 210000, EndMs: 220000},
	}}
	if err := db.PublishRecordingMedia(t.Context(), fragmented.ID, index); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(t.Context(), `UPDATE recording_segments SET status='failed' WHERE id=?`, fragmented.ID); err != nil {
		t.Fatal(err)
	}
	open(7, 300, 0, "recording") // Active, but no completed fragment is publishable.
	open(7, 160, 170, "failed")  // Unindexed failed files are not playable.
	open(7, 1, 999, "deleted")
	open(8, 1, 900, "ready") // Its wider extent must not affect camera 7.

	spans, err := db.PlaybackSpans(t.Context(), 7, 100000, 215000)
	if err != nil || len(spans) != 2 {
		t.Fatalf("spans=%+v: %v", spans, err)
	}
	if spans[0].SegmentID != legacy.ID || spans[0].StartMs != 90000 || spans[0].EndMs != 150000 || spans[0].Fragmented {
		t.Fatalf("legacy span changed: %+v", spans[0])
	}
	// The earlier fragment extends beyond the last fragment and the requested
	// window. Neither may truncate the complete published file interval.
	if spans[1].SegmentID != fragmented.ID || spans[1].StartMs != 200000 || spans[1].EndMs != 260000 || !spans[1].Fragmented || spans[1].Status != "failed" {
		t.Fatalf("published extent changed: %+v", spans[1])
	}
	end, active, err := db.PlaybackBounds(t.Context(), 7)
	if err != nil || end == nil || *end != 260000 || !active {
		t.Fatalf("bounds=%v active=%v: %v", end, active, err)
	}
	previous, next, err := db.PlaybackNeighbors(t.Context(), 7, 175000)
	if err != nil || previous == nil || *previous != 150000 || next == nil || *next != 200000 {
		t.Fatalf("neighbors=%v,%v: %v", previous, next, err)
	}
	page, err := db.PlaybackSpansPage(t.Context(), 7, 100000, 250000, 0, 0, 1)
	if err != nil || len(page) != 1 || page[0].SegmentID != fragmented.ID || page[0].EndMs != 260000 {
		t.Fatalf("first page=%+v: %v", page, err)
	}
	page, err = db.PlaybackSpansPage(t.Context(), 7, 100000, 250000, page[0].StartMs, page[0].SegmentID, 1)
	if err != nil || len(page) != 1 || page[0].SegmentID != legacy.ID {
		t.Fatalf("next page=%+v: %v", page, err)
	}
	spans, err = db.PlaybackSpans(t.Context(), 7, 150000, 200000)
	if err != nil || len(spans) != 0 {
		t.Fatalf("half-open gap=%+v: %v", spans, err)
	}
	end, active, err = db.PlaybackBounds(t.Context(), 8)
	if err != nil || end == nil || *end != 900000 || active {
		t.Fatalf("other camera bounds=%v active=%v: %v", end, active, err)
	}
	end, active, err = db.PlaybackBounds(t.Context(), 9)
	if err != nil || end != nil || active {
		t.Fatalf("empty camera bounds=%v active=%v: %v", end, active, err)
	}
}
