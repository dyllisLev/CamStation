package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"camstation/internal/recordingmedia"
)

func TestLiveReadsSeeCommittedDataWhileWriterIsHeld(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{Name: "yard", URL: "rtsp://camera/input", StreamName: "yard"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateLayout(t.Context(), LayoutProfile{ID: "live", Name: "live"}); err != nil {
		t.Fatal(err)
	}
	segment, err := db.OpenRecordingSegmentUnique(t.Context(), RecordingSegment{CameraID: camera.ID, StreamName: "yard", Filename: "one", TSStart: 100, Status: "recording"})
	if err != nil {
		t.Fatal(err)
	}
	idx := recordingmedia.Index{InitLength: 10, CommittedOffset: 30, VideoCodec: "avc1", TimeBasis: recordingmedia.TimeBasis, Fragments: []recordingmedia.Fragment{
		{Sequence: 1, Offset: 10, Length: 20, MediaStartMs: 0, MediaEndMs: 1000, StartMs: 100000, EndMs: 101000},
	}}
	if err := db.PublishRecordingMedia(t.Context(), segment.ID, idx); err != nil {
		t.Fatal(err)
	}
	// Reopen readers during an outstanding write, as on a fresh live request.
	db.readDB.SetMaxIdleConns(0)
	db.readDB.SetMaxIdleConns(4)
	tx, err := db.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`UPDATE cameras SET enabled=0 WHERE stream_name='yard'`,
		`UPDATE layouts SET name='changed' WHERE id='live'`,
		`UPDATE recording_media SET committed_offset=50`,
		`INSERT INTO recording_fragments SELECT segment_id,2,30,20,1000,2000,101000,102000 FROM recording_media`,
	} {
		if _, err := tx.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	// This is a dependency test, not a microbenchmark: the writer cannot finish
	// until every live read has returned. Sharing its connection must time out.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	registered, err := db.IsRegisteredPublicStream(ctx, "yard")
	if err != nil || !registered {
		t.Fatalf("registration waited or saw uncommitted disable: %v %v", registered, err)
	}
	cameras, err := db.ListCameras(ctx, false)
	if err != nil || len(cameras) != 1 || !cameras[0].Enabled || len(cameras[0].Outputs) != 3 {
		t.Fatalf("camera configuration while writing: count=%d err=%v", len(cameras), err)
	}
	if _, err := db.GetCameraByStream(ctx, "yard"); err != nil {
		t.Fatal(err)
	}
	layouts, err := db.ListLayouts(ctx)
	if err != nil || len(layouts) != 1 || layouts[0].Name != "live" {
		t.Fatalf("layouts while writing: %+v %v", layouts, err)
	}
	if _, err := db.GetLayout(ctx, "live"); err != nil {
		t.Fatal(err)
	}
	spans, err := db.PlaybackSpans(ctx, camera.ID, 100000, 103000)
	if err != nil || len(spans) != 1 || spans[0].EndMs != 101000 {
		t.Fatalf("published interval while writing: %+v %v", spans, err)
	}
	_, media, err := db.RecordingMedia(ctx, segment.ID)
	if err != nil || media.CommittedOffset != 30 || len(media.Fragments) != 1 {
		t.Fatalf("published bytes while writing: %+v %v", media, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	registered, err = db.IsRegisteredPublicStream(t.Context(), "yard")
	if err != nil || registered {
		t.Fatalf("committed disable not visible: %v %v", registered, err)
	}
	spans, err = db.PlaybackSpans(t.Context(), camera.ID, 100000, 103000)
	if err != nil || len(spans) != 1 || spans[0].EndMs != 102000 {
		t.Fatalf("committed publication not visible: %+v %v", spans, err)
	}
	_, media, err = db.RecordingMedia(t.Context(), segment.ID)
	if err != nil || media.CommittedOffset != 50 || len(media.Fragments) != 2 {
		t.Fatalf("committed byte index not visible: %+v %v", media, err)
	}
}

func TestReaderConnectionsRemainReadOnlyAfterReplacementAndReopen(t *testing.T) {
	// URI punctuation must refer to the same filename on both pools.
	path := filepath.Join(t.TempDir(), "archive ?#% 한글.db")
	for reopen := 0; reopen < 2; reopen++ {
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		for replacement := 0; replacement < 2; replacement++ {
			var connections []*sql.Conn
			for i := 0; i < 4; i++ {
				conn, err := db.readDB.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
				connections = append(connections, conn)
				var readOnly, version int
				if err := conn.QueryRowContext(t.Context(), `PRAGMA query_only`).Scan(&readOnly); err != nil || readOnly != 1 {
					t.Fatalf("reader not query-only: %d %v", readOnly, err)
				}
				if err := conn.QueryRowContext(t.Context(), `SELECT version FROM schema_migrations WHERE version=?`, playbackLookupMigration).Scan(&version); err != nil {
					t.Fatal(err)
				}
				if _, err := conn.ExecContext(t.Context(), `DELETE FROM schema_migrations`); err == nil {
					t.Fatal("reader accepted a write")
				}
			}
			for _, conn := range connections {
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			}
			db.readDB.SetMaxIdleConns(0)
			db.readDB.SetMaxIdleConns(4)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
