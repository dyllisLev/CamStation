package store

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"camstation/internal/recordingmedia"
)

func TestPlaybackCameraLookupPreservesHistoricalOwnerOrder(t *testing.T) {
	db := newSettingsJobTestDB(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{Name: "current", URL: "rtsp://camera/input", StreamName: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	mustPlaybackExec(t, db, `INSERT INTO recording_segments(camera_id,stream_name,filename,ts_start,status,created_at,updated_at) VALUES
 (7,'shared','old',1,'ready',0,0),(8,'shared','later',2,'ready',0,0),
 (3,'shared','deleted',3,'deleted',0,0),(9,'archived','gone',4,'ready',0,0)`)
	for _, test := range []struct {
		key string
		id  int64
	}{{"shared", camera.ID}, {"archived", 9}, {"camera:7", 7}, {"camera:9", 9}} {
		got, err := db.PlaybackCameraID(t.Context(), test.key)
		if err != nil || got != test.id {
			t.Fatalf("key=%s got=%d want=%d: %v", test.key, got, test.id, err)
		}
	}
	if _, err := db.DeleteCamera(t.Context(), "shared"); err != nil {
		t.Fatal(err)
	}
	got, err := db.PlaybackCameraID(t.Context(), "shared")
	if err != nil || got != 7 {
		t.Fatalf("archived shared owner=%d: %v", got, err)
	}
	mustPlaybackExec(t, db, `INSERT INTO cameras(id,name,url,stream_name,state,created_at,updated_at) VALUES (100,'replacement','rtsp://camera/input','shared','unknown','','')`)
	got, err = db.PlaybackCameraID(t.Context(), "shared")
	if err != nil || got != 7 {
		t.Fatalf("shared key must retain the lowest historical owner: %d %v", got, err)
	}
	for _, key := range []string{"missing", "camera:3", "camera:99"} {
		if _, err := db.PlaybackCameraID(t.Context(), key); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("key %s should be absent: %v", key, err)
		}
	}
}

// Build the pre-lookup schema in an isolated test DB. This exercises the real
// upgrade/backfill rather than populating already-maintained summary columns.
func removePlaybackLookupSchema(t testing.TB, db *DB) {
	t.Helper()
	rows, err := db.db.Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'recording_playback_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range triggers {
		mustPlaybackExec(t, db, `DROP TRIGGER `+name)
	}
	for _, query := range []string{
		`DROP TABLE recording_playback_ranges`,
		`DROP INDEX idx_recording_playback_start`,
		`DROP INDEX idx_recording_playback_end`,
		`DROP INDEX idx_recording_camera_active`,
		`DROP INDEX idx_recording_camera_archive`,
		`DROP INDEX idx_recording_stream_archive`,
		`ALTER TABLE recording_segments DROP COLUMN playback_start_ms`,
		`ALTER TABLE recording_segments DROP COLUMN playback_end_ms`,
		`ALTER TABLE recording_segments DROP COLUMN playback_fragmented`,
		`DELETE FROM schema_migrations WHERE version=20260911`,
	} {
		mustPlaybackExec(t, db, query)
	}
}

func mustPlaybackExec(t testing.TB, db *DB, query string, args ...any) {
	t.Helper()
	if _, err := db.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackLookupMigrationPreservesLegacyAndPublishedExtrema(t *testing.T) {
	db := newSettingsJobTestDB(t)
	removePlaybackLookupSchema(t, db)
	for _, query := range []string{
		`INSERT INTO recording_segments(id,camera_id,stream_name,filename,ts_start,ts_end,status,backup_state,created_at,updated_at) VALUES
 (1,7,'archive','legacy',90.12345,150.23456,'ready','backed_up',0,0),
 (2,7,'archive','failed-published',200,220,'failed','pending',0,0),
 (3,7,'archive','unpublished',300,999,'recording','pending',0,0),
 (4,7,'archive','failed-legacy',160,170,'failed','pending',0,0),
 (5,7,'archive','deleted-published',400,900,'deleted','backed_up',0,0),
 (6,7,'archive','empty-index',500,999,'ready','pending',0,0),
 (7,8,'other','other-camera',1,999,'ready','pending',0,0)`,
		`INSERT INTO recording_media VALUES (2,10,50,'avc1','media_epoch'),(5,10,30,'avc1','media_epoch'),(6,10,10,'avc1','media_epoch')`,
		`INSERT INTO recording_fragments VALUES
 (2,1,10,20,0,2000,200000,260000),(2,2,30,20,2000,4000,210000,220000),
 (5,1,10,20,0,2000,400000,900000)`,
	} {
		mustPlaybackExec(t, db, query)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []PlaybackSpan{
		{SegmentID: 1, StartMs: 90123, EndMs: 150235, Status: "ready", TimeBasis: "legacy_filename"},
		{SegmentID: 2, StartMs: 200000, EndMs: 260000, Status: "failed", Fragmented: true, TimeBasis: "media_epoch", VideoCodec: "avc1"},
	}
	for pass := 0; pass < 2; pass++ {
		got, err := db.PlaybackSpans(t.Context(), 7, 0, 1000000)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("migration pass %d: got=%+v want=%+v err=%v", pass, got, want, err)
		}
		end, active, err := db.PlaybackBounds(t.Context(), 7)
		if err != nil || end == nil || *end != 260000 || !active {
			t.Fatalf("migration bounds=%v active=%v: %v", end, active, err)
		}
		legacy, err := db.GetRecordingSegmentByID(t.Context(), 1)
		if err != nil || legacy.BackupState != "backed_up" || legacy.TSStart != 90.12345 || legacy.TSEnd == nil || *legacy.TSEnd != 150.23456 {
			t.Fatalf("migration changed canonical recording: %+v %v", legacy, err)
		}
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	// A failed staged deletion can restore the saved published interval without
	// reconstructing it from the parent file's (different) close timestamp.
	if err := db.MarkRecordingSegmentStatusByID(t.Context(), 5, "ready", "restore"); err != nil {
		t.Fatal(err)
	}
	spans, err := db.PlaybackSpans(t.Context(), 7, 800000, 850000)
	if err != nil || len(spans) != 1 || spans[0].SegmentID != 5 || spans[0].EndMs != 900000 {
		t.Fatalf("restored published span=%+v: %v", spans, err)
	}
	var integrity string
	if err := db.db.QueryRow(`SELECT rtreecheck('recording_playback_ranges')`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("range index integrity=%q: %v", integrity, err)
	}
}

func TestPlaybackLookupMigrationRollsBackAndCanRetry(t *testing.T) {
	db := newSettingsJobTestDB(t)
	removePlaybackLookupSchema(t, db)
	// Force a failure after ALTER/backfill but before migration completion.
	mustPlaybackExec(t, db, `CREATE INDEX idx_recording_playback_end ON recording_segments(filename)`)
	if err := db.ensurePlaybackLookupSchema(t.Context()); err == nil {
		t.Fatal("expected migration failure")
	}
	var columns, applied int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('recording_segments') WHERE name LIKE 'playback_%'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, playbackLookupMigration).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if columns != 0 || applied != 0 {
		t.Fatalf("partial upgrade survived rollback: columns=%d applied=%d", columns, applied)
	}
	mustPlaybackExec(t, db, `DROP INDEX idx_recording_playback_end`)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackLookupPublicationStateChangesAndRollback(t *testing.T) {
	db := newSettingsJobTestDB(t)
	segment, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: 7, StreamName: "archive", Filename: "one", TSStart: 100, Status: "recording"})
	if err != nil {
		t.Fatal(err)
	}
	index := recordingmedia.Index{InitLength: 10, CommittedOffset: 50, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{
		{Sequence: 1, Offset: 10, Length: 20, MediaStartMs: 0, MediaEndMs: 1000, StartMs: 100000, EndMs: 160000},
		{Sequence: 2, Offset: 30, Length: 20, MediaStartMs: 1000, MediaEndMs: 2000, StartMs: 110000, EndMs: 120000},
	}}
	for i := 0; i < 2; i++ {
		if err := db.PublishRecordingMedia(t.Context(), segment.ID, index); err != nil {
			t.Fatal(err)
		}
	}
	index.CommittedOffset = 70
	index.Fragments = append(index.Fragments, recordingmedia.Fragment{Sequence: 3, Offset: 50, Length: 20, MediaStartMs: 2000, MediaEndMs: 3000, StartMs: 120000, EndMs: 130000})
	if err := db.PublishRecordingMedia(t.Context(), segment.ID, index); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"recording", "finalizing", "failed", "ready", "deleted", "ready"} {
		if err := db.MarkRecordingSegmentStatusByID(t.Context(), segment.ID, status, "test transition"); err != nil {
			t.Fatal(err)
		}
		spans, err := db.PlaybackSpans(t.Context(), 7, 150000, 155000)
		if err != nil {
			t.Fatal(err)
		}
		if status == "deleted" {
			if len(spans) != 0 {
				t.Fatalf("deleted recording advertised: %+v", spans)
			}
		} else if len(spans) != 1 || spans[0].EndMs != 160000 || spans[0].Status != status {
			t.Fatalf("status %s lost full published extrema: %+v", status, spans)
		}
	}
	// Publication may fail at the final canonical timestamp update, after the
	// fragment trigger already changed the summary and R-tree. All must roll back.
	unpublished, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: 7, StreamName: "archive", Filename: "two", TSStart: 200, Status: "recording"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PublishRecordingMedia(t.Context(), unpublished.ID, index); err == nil {
		t.Fatal("expected identity collision at publication commit")
	}
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM recording_fragments WHERE segment_id=?`, unpublished.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back fragments=%d: %v", count, err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM recording_playback_ranges WHERE segment_id=?`, unpublished.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back interval=%d: %v", count, err)
	}
}

func TestPlaybackLookupExactIntervalsSurviveRTreeRounding(t *testing.T) {
	for _, start := range []int64{1789113600123, 2147483648123, 253402299000123} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			db := newSettingsJobTestDB(t)
			// Adjacent large IDs and epoch milliseconds are not exactly
			// representable by the R-tree's float coordinates.
			for _, camera := range []int64{16777217, 16777218} {
				segment, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: camera, StreamName: fmt.Sprintf("camera-%d", camera), Filename: "one", TSStart: 1, Status: "recording"})
				if err != nil {
					t.Fatal(err)
				}
				idx := recordingmedia.Index{InitLength: 10, CommittedOffset: 30, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{
					{Sequence: 1, Offset: 10, Length: 20, MediaStartMs: 0, MediaEndMs: 1000, StartMs: start, EndMs: start + 1000},
				}}
				if err := db.PublishRecordingMedia(t.Context(), segment.ID, idx); err != nil {
					t.Fatal(err)
				}
			}
			for _, r := range []struct {
				from, to int64
				count    int
			}{
				{start - 1, start, 0}, {start, start + 1, 1}, {start + 999, start + 1000, 1}, {start + 1000, start + 1001, 0},
			} {
				spans, err := db.PlaybackSpans(t.Context(), 16777217, r.from, r.to)
				if err != nil || len(spans) != r.count {
					t.Fatalf("[%d,%d) got=%+v want=%d: %v", r.from, r.to, spans, r.count, err)
				}
			}
		})
	}
}

func TestPlaybackLookupLongOverlapAndEqualStartPagination(t *testing.T) {
	db := newSettingsJobTestDB(t)
	end := float64(200 * 24 * 60 * 60)
	long, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: 7, StreamName: "long", Filename: "long", TSStart: 1, TSEnd: &end, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	start := float64(100 * 24 * 60 * 60)
	shortEnd := start + 60
	var ids []int64
	for i := 0; i < 3; i++ {
		s, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: 7, StreamName: fmt.Sprintf("short-%d", i), Filename: "short", TSStart: start, TSEnd: &shortEnd, Status: "ready"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	from, to := int64(start*1000)+1, int64(start*1000)+2
	spans, err := db.PlaybackSpans(t.Context(), 7, from, to)
	if err != nil || len(spans) != 4 || spans[0].SegmentID != long.ID || spans[0].StartMs != 1000 || spans[0].EndMs != int64(end*1000) {
		t.Fatalf("long overlapping file was truncated or missed: %+v %v", spans, err)
	}
	var beforeMs, beforeID int64
	for _, wantID := range []int64{ids[2], ids[1], ids[0], long.ID} {
		page, err := db.PlaybackSpansPage(t.Context(), 7, from, to, beforeMs, beforeID, 1)
		if err != nil || len(page) != 1 || page[0].SegmentID != wantID {
			t.Fatalf("equal-start cursor got=%+v want=%d: %v", page, wantID, err)
		}
		beforeMs, beforeID = page[0].StartMs, page[0].SegmentID
	}
	page, err := db.PlaybackSpansPage(t.Context(), 7, from, to, beforeMs, beforeID, 1)
	if err != nil || len(page) != 0 {
		t.Fatalf("exhausted cursor: %+v %v", page, err)
	}
	for _, status := range []string{"recording", "failed", "deleted", "ready"} {
		if err := db.MarkRecordingSegmentStatusByID(t.Context(), long.ID, status, "test legacy state"); err != nil {
			t.Fatal(err)
		}
		spans, err := db.PlaybackSpans(t.Context(), 7, int64(end*1000)-2, int64(end*1000)-1)
		want := 0
		if status == "ready" {
			want = 1
		}
		if err != nil || len(spans) != want {
			t.Fatalf("legacy status %s: spans=%+v err=%v", status, spans, err)
		}
	}
}

func TestPlaybackLookupPlansUseIntervalAndBoundaryIndexes(t *testing.T) {
	db := newSettingsJobTestDB(t)
	for _, test := range []struct {
		name, query string
		args        []any
		want        []string
	}{
		{"overlap", playbackRangeQuery + ` ORDER BY s.playback_start_ms,s.id`, []any{1, 1000, 2000}, []string{"SCAN r VIRTUAL TABLE INDEX", "SEARCH s USING INTEGER PRIMARY KEY"}},
		{"bounds", playbackBoundsQuery, []any{1}, []string{"idx_recording_playback_end", "idx_recording_camera_active"}},
		{"neighbors", playbackNeighborsQuery, []any{1, 1000}, []string{"idx_recording_playback_end", "idx_recording_playback_start"}},
		{"archived camera", playbackArchivedCameraQuery, []any{1}, []string{"idx_recording_camera_archive", "SEARCH cameras USING INTEGER PRIMARY KEY"}},
		{"stream camera", playbackStreamCameraQuery, []any{"archive"}, []string{"idx_recording_stream_archive"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := db.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+test.query, test.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var plan []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			text := strings.Join(plan, "\n")
			for _, want := range test.want {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q in plan:\n%s", want, text)
				}
			}
			for _, bad := range []string{"recording_fragments", "SCAN recording_segments", "SCAN s", "USE TEMP B-TREE FOR GROUP BY"} {
				if strings.Contains(text, bad) {
					t.Fatalf("archive scan %q in plan:\n%s", bad, text)
				}
			}
			t.Log(text)
		})
	}
}
