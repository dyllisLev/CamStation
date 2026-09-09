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
	rows, err := d.db.QueryContext(ctx, `WITH archived AS (
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
		err = d.db.QueryRowContext(ctx, `SELECT id FROM cameras WHERE id=? UNION SELECT camera_id FROM recording_segments WHERE camera_id=? AND status!='deleted' LIMIT 1`, id, id).Scan(&found)
		return found, err
	}
	var id int64
	err := d.db.QueryRowContext(ctx, `SELECT id FROM cameras c WHERE stream_name=? OR recording_stream_name=? OR live_stream_name=? OR EXISTS(SELECT 1 FROM camera_outputs o WHERE o.camera_id=c.id AND o.stream_name=?) UNION SELECT camera_id FROM recording_segments WHERE stream_name=? AND status!='deleted' LIMIT 1`, key, key, key, key, key).Scan(&id)
	return id, err
}

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

const playbackSpansCTE = `WITH spans AS (
 SELECT s.id,s.camera_id,s.status,CASE WHEN m.segment_id IS NULL THEN CAST(ROUND(s.ts_start*1000) AS INTEGER) ELSE MIN(f.start_ms) END start_ms,
 CASE WHEN m.segment_id IS NULL THEN CAST(ROUND(s.ts_end*1000) AS INTEGER) ELSE MAX(f.end_ms) END end_ms,
 m.segment_id IS NOT NULL fragmented,COALESCE(m.time_basis,'legacy_filename') time_basis,COALESCE(m.video_codec,'') video_codec
 FROM recording_segments s LEFT JOIN recording_media m ON m.segment_id=s.id LEFT JOIN recording_fragments f ON f.segment_id=s.id
 WHERE s.status IN ('ready','recording','finalizing','failed') AND (m.segment_id IS NOT NULL OR (s.status='ready' AND s.ts_end>s.ts_start))
 GROUP BY s.id
 ) `

func scanPlaybackSpan(row scanner) (PlaybackSpan, error) {
	var s PlaybackSpan
	err := row.Scan(&s.SegmentID, &s.StartMs, &s.EndMs, &s.Status, &s.Fragmented, &s.TimeBasis, &s.VideoCodec)
	return s, err
}

const playbackSpanColumns = "id,start_ms,end_ms,status,fragmented,time_basis,video_codec"

func (d *DB) PlaybackSpans(ctx context.Context, cameraID, fromMs, toMs int64) ([]PlaybackSpan, error) {
	rows, err := d.db.QueryContext(ctx, playbackSpansCTE+`SELECT `+playbackSpanColumns+` FROM spans WHERE camera_id=? AND end_ms>? AND start_ms<? ORDER BY start_ms,id`, cameraID, fromMs, toMs)
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
	err := d.db.QueryRowContext(ctx, playbackSpansCTE+`SELECT (SELECT MAX(end_ms) FROM spans WHERE camera_id=?),EXISTS(SELECT 1 FROM recording_segments WHERE camera_id=? AND status IN ('recording','finalizing'))`, cameraID, cameraID).Scan(&end, &active)
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
	err := d.db.QueryRowContext(ctx, playbackSpansCTE+`SELECT MAX(CASE WHEN end_ms<=? THEN end_ms END),MIN(CASE WHEN start_ms>? THEN start_ms END) FROM spans WHERE camera_id=?`, atMs, atMs, cameraID).Scan(&previous, &next)
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

func PlaybackMediaID(s PlaybackSpan) string {
	kind := "file"
	if s.Fragmented {
		kind = "fmp4"
	}
	return fmt.Sprintf("%s-%d", kind, s.SegmentID)
}

var ErrPlaybackMediaDeleted = errors.New("playback media deleted")

func (d *DB) PlaybackSpansPage(ctx context.Context, cameraID, fromMs, toMs, beforeMs, beforeID int64, limit int) ([]PlaybackSpan, error) {
	query := playbackSpansCTE + `SELECT ` + playbackSpanColumns + ` FROM spans WHERE camera_id=? AND end_ms>? AND start_ms<?`
	args := []any{cameraID, fromMs, toMs}
	if beforeID > 0 {
		query += ` AND (start_ms<? OR (start_ms=? AND id<?))`
		args = append(args, beforeMs, beforeMs, beforeID)
	}
	query += ` ORDER BY start_ms DESC,id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, query, args...)
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
	err := d.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM recording_segments WHERE camera_id=? AND status IN ('recording','finalizing') AND ts_start<=?)`, cameraID, float64(atMs)/1000).Scan(&waiting)
	return waiting, err
}
