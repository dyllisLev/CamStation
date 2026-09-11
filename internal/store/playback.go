package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PlaybackCamera contains only catalogue-safe values. Archived cameras remain selectable.
type PlaybackCamera struct {
	CameraKey     string `json:"cameraKey"`
	Name          string `json:"name"`
	Registered    bool   `json:"registered"`
	HasRecordings bool   `json:"hasRecordings"`
	CameraID      int64  `json:"-"`
}

func (d *DB) ListPlaybackCameras(ctx context.Context) ([]PlaybackCamera, error) {
	rows, err := d.readDB.QueryContext(ctx, `WITH archived AS (
 SELECT camera_id,MAX(NULLIF(camera_name,'')) AS name FROM recording_segments WHERE status!='deleted' GROUP BY camera_id
 ), ids AS (SELECT id FROM cameras UNION SELECT camera_id FROM archived)
 SELECT ids.id,COALESCE(c.stream_name,'camera:'||ids.id),COALESCE(NULLIF(c.name,''),a.name,'보관 카메라 #'||ids.id),c.id IS NOT NULL,a.camera_id IS NOT NULL
 FROM ids LEFT JOIN cameras c ON c.id=ids.id LEFT JOIN archived a ON a.camera_id=ids.id ORDER BY c.id IS NULL,ids.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PlaybackCamera, 0)
	for rows.Next() {
		var c PlaybackCamera
		if err := rows.Scan(&c.CameraID, &c.CameraKey, &c.Name, &c.Registered, &c.HasRecordings); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (d *DB) PlaybackCameraID(ctx context.Context, key string) (int64, error) {
	if strings.HasPrefix(key, "camera:") {
		id, err := strconv.ParseInt(strings.TrimPrefix(key, "camera:"), 10, 64)
		if err != nil || id <= 0 {
			return 0, sql.ErrNoRows
		}
		var found int64
		err = d.readDB.QueryRowContext(ctx, playbackArchivedCameraQuery, id).Scan(&found)
		return found, err
	}
	var id int64
	err := d.readDB.QueryRowContext(ctx, playbackStreamCameraQuery, key).Scan(&id)
	return id, err
}

// Preserve the original UNION's lowest-ID resolution when a stream key has
// historical owners, but read at most one archived owner from its index.
const playbackStreamCameraQuery = `SELECT MIN(id) FROM (
 SELECT (SELECT id FROM cameras c WHERE stream_name=?1 OR recording_stream_name=?1 OR live_stream_name=?1
  OR EXISTS(SELECT 1 FROM camera_outputs o WHERE o.camera_id=c.id AND o.stream_name=?1) ORDER BY id LIMIT 1) AS id
 UNION ALL
 SELECT (SELECT camera_id FROM recording_segments WHERE stream_name=?1 AND status!='deleted' ORDER BY camera_id LIMIT 1)
) HAVING MIN(id) IS NOT NULL`

// Resolving an archived key only needs existence, not a deduplicated collection
// of every file ever recorded by that camera.
const playbackArchivedCameraQuery = `SELECT ?1 WHERE EXISTS(SELECT 1 FROM cameras WHERE id=?1)
 OR EXISTS(SELECT 1 FROM recording_segments WHERE camera_id=?1 AND status!='deleted')`

// PlaybackSpan is a coalesced file interval, never the uncommitted end of a growing file.
type PlaybackSpan struct {
	SegmentID  int64
	StartMs    int64
	EndMs      int64
	Status     string
	Fragmented bool
	TimeBasis  string
	VideoCodec string
}

// Start with the interval index even for an old or middle-of-history window.
// A simple start/end B-tree can only bound one side of an overlap query and can
// still scan years of unrelated files. CROSS JOIN keeps the candidate index in
// the outer loop. Its outward-rounded coordinates need exact integer rechecks.
// Complete published file extrema are maintained at write time; these reads
// never join or aggregate recording_fragments.
const playbackRangeQuery = `SELECT s.id,s.playback_start_ms,s.playback_end_ms,s.status,s.playback_fragmented,
 COALESCE(m.time_basis,'legacy_filename'),COALESCE(m.video_codec,'')
 FROM recording_playback_ranges r CROSS JOIN recording_segments s ON s.id=r.segment_id
 LEFT JOIN recording_media m ON m.segment_id=s.id
 WHERE r.camera_min<=?1 AND r.camera_max>=?1 AND r.end_ms>?2 AND r.start_ms<?3
 AND s.camera_id=?1 AND ` + playbackAvailable + ` AND s.playback_end_ms>?2 AND s.playback_start_ms<?3`

func scanPlaybackSpan(row scanner) (PlaybackSpan, error) {
	var s PlaybackSpan
	err := row.Scan(&s.SegmentID, &s.StartMs, &s.EndMs, &s.Status, &s.Fragmented, &s.TimeBasis, &s.VideoCodec)
	return s, err
}

func (d *DB) PlaybackSpans(ctx context.Context, cameraID, fromMs, toMs int64) ([]PlaybackSpan, error) {
	rows, err := d.readDB.QueryContext(ctx, playbackRangeQuery+` ORDER BY s.playback_start_ms,s.id`, cameraID, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlaybackSpan, 0)
	for rows.Next() {
		s, err := scanPlaybackSpan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) PlaybackBounds(ctx context.Context, cameraID int64) (*int64, bool, error) {
	var end sql.NullInt64
	var active bool
	err := d.readDB.QueryRowContext(ctx, playbackBoundsQuery, cameraID).Scan(&end, &active)
	if err != nil {
		return nil, false, err
	}
	if !end.Valid {
		return nil, active, nil
	}
	return &end.Int64, active, nil
}

func (d *DB) PlaybackNeighbors(ctx context.Context, cameraID, atMs int64) (*int64, *int64, error) {
	var previous, next sql.NullInt64
	err := d.readDB.QueryRowContext(ctx, playbackNeighborsQuery, cameraID, atMs).Scan(&previous, &next)
	if err != nil {
		return nil, nil, err
	}
	var p, n *int64
	if previous.Valid {
		p = &previous.Int64
	}
	if next.Valid {
		n = &next.Int64
	}
	return p, n, nil
}

const playbackBoundsQuery = `SELECT
 (SELECT playback_end_ms FROM recording_segments WHERE camera_id=?1 AND ` + playbackAvailable + ` ORDER BY playback_end_ms DESC LIMIT 1),
 EXISTS(SELECT 1 FROM recording_segments WHERE camera_id=?1 AND status IN ('recording','finalizing'))`

const playbackNeighborsQuery = `SELECT
 (SELECT playback_end_ms FROM recording_segments WHERE camera_id=?1 AND ` + playbackAvailable + ` AND playback_end_ms<=?2 ORDER BY playback_end_ms DESC LIMIT 1),
 (SELECT playback_start_ms FROM recording_segments WHERE camera_id=?1 AND ` + playbackAvailable + ` AND playback_start_ms>?2 ORDER BY playback_start_ms LIMIT 1)`

func PlaybackMediaID(s PlaybackSpan) string {
	kind := "file"
	if s.Fragmented {
		kind = "fmp4"
	}
	return fmt.Sprintf("%s-%d", kind, s.SegmentID)
}

var ErrPlaybackMediaDeleted = errors.New("playback media deleted")

func (d *DB) PlaybackSpansPage(ctx context.Context, cameraID, fromMs, toMs, beforeMs, beforeID int64, limit int) ([]PlaybackSpan, error) {
	query := playbackRangeQuery
	args := []any{cameraID, fromMs, toMs}
	if beforeID > 0 {
		query += ` AND r.start_ms<=?4 AND (s.playback_start_ms<?4 OR (s.playback_start_ms=?4 AND s.id<?5))`
		args = append(args, beforeMs, beforeID)
	}
	query += ` ORDER BY s.playback_start_ms DESC,s.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlaybackSpan, 0)
	for rows.Next() {
		s, err := scanPlaybackSpan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) PlaybackWaitingAt(ctx context.Context, cameraID, atMs int64) (bool, error) {
	var waiting bool
	err := d.readDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM recording_segments WHERE camera_id=? AND status IN ('recording','finalizing') AND ts_start<=?)`, cameraID, float64(atMs)/1000).Scan(&waiting)
	return waiting, err
}
