package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"camstation/internal/recordingmedia"
)

// WithRecordingMediaLock makes the normal archive move and its DB path switch
// indivisible to new playback opens. Already-open Unix file handles survive moves.
func (d *DB) WithRecordingMediaLock(fn func() error) error {
	d.recordingMediaMu.Lock()
	defer d.recordingMediaMu.Unlock()
	return fn()
}

func (d *DB) PublishRecordingMedia(ctx context.Context, segmentID int64, index recordingmedia.Index) error {
	if len(index.Fragments) == 0 {
		return nil
	}
	if index.InitLength <= 0 || index.CommittedOffset < index.InitLength || index.TimeBasis == "" {
		return errors.New("invalid recording media index")
	}
	for i, f := range index.Fragments {
		if f.Sequence < 0 || f.Offset < index.InitLength || f.Length <= 0 || f.Offset > index.CommittedOffset-f.Length || f.EndMs <= f.StartMs || f.MediaEndMs <= f.MediaStartMs {
			return errors.New("invalid completed recording fragment")
		}
		if i > 0 {
			p := index.Fragments[i-1]
			if f.Sequence <= p.Sequence || f.Offset < p.Offset+p.Length || f.StartMs < p.StartMs {
				return errors.New("recording fragment order regressed")
			}
		}
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM recording_segments WHERE id=?`, segmentID).Scan(&status); err != nil {
		return err
	}
	if status == "deleted" {
		return ErrPlaybackMediaDeleted
	}
	var oldInit, oldCommitted int64
	var oldCodec, oldBasis string
	err = tx.QueryRowContext(ctx, `SELECT init_length,committed_offset,video_codec,time_basis FROM recording_media WHERE segment_id=?`, segmentID).Scan(&oldInit, &oldCommitted, &oldCodec, &oldBasis)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && (oldInit != index.InitLength || oldCommitted > index.CommittedOffset || oldCodec != index.VideoCodec || oldBasis != index.TimeBasis) {
		return errors.New("published recording media identity changed")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO recording_media(segment_id,init_length,committed_offset,video_codec,time_basis) VALUES(?,?,?,?,?) ON CONFLICT(segment_id) DO UPDATE SET committed_offset=excluded.committed_offset`, segmentID, index.InitLength, index.CommittedOffset, index.VideoCodec, index.TimeBasis)
	if err != nil {
		return err
	}
	for _, f := range index.Fragments {
		var old recordingmedia.Fragment
		err = tx.QueryRowContext(ctx, `SELECT sequence,byte_offset,byte_length,media_start_ms,media_end_ms,start_ms,end_ms FROM recording_fragments WHERE segment_id=? AND sequence=?`, segmentID, f.Sequence).Scan(&old.Sequence, &old.Offset, &old.Length, &old.MediaStartMs, &old.MediaEndMs, &old.StartMs, &old.EndMs)
		if err == nil {
			if old != f {
				return errors.New("published recording fragment changed")
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recording_fragments(segment_id,sequence,byte_offset,byte_length,media_start_ms,media_end_ms,start_ms,end_ms) VALUES(?,?,?,?,?,?,?,?)`, segmentID, f.Sequence, f.Offset, f.Length, f.MediaStartMs, f.MediaEndMs, f.StartMs, f.EndMs)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE recording_segments SET ts_start=?,ts_end=? WHERE id=?`, float64(index.Fragments[0].StartMs)/1000, float64(index.Fragments[len(index.Fragments)-1].EndMs)/1000, segmentID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) RecordingMedia(ctx context.Context, segmentID int64) (RecordingSegment, recordingmedia.Index, error) {
	s, err := d.GetRecordingSegmentByID(ctx, segmentID)
	if err != nil {
		return s, recordingmedia.Index{}, err
	}
	if s.Status == "deleted" {
		return s, recordingmedia.Index{}, ErrPlaybackMediaDeleted
	}
	var idx recordingmedia.Index
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return s, idx, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT init_length,committed_offset,video_codec,time_basis FROM recording_media WHERE segment_id=?`, segmentID).Scan(&idx.InitLength, &idx.CommittedOffset, &idx.VideoCodec, &idx.TimeBasis)
	if err != nil {
		return s, idx, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,byte_offset,byte_length,media_start_ms,media_end_ms,start_ms,end_ms FROM recording_fragments WHERE segment_id=? ORDER BY sequence`, segmentID)
	if err != nil {
		return s, idx, err
	}
	defer rows.Close()
	for rows.Next() {
		var f recordingmedia.Fragment
		if err := rows.Scan(&f.Sequence, &f.Offset, &f.Length, &f.MediaStartMs, &f.MediaEndMs, &f.StartMs, &f.EndMs); err != nil {
			return s, idx, err
		}
		idx.Fragments = append(idx.Fragments, f)
	}
	if err := rows.Err(); err != nil {
		return s, idx, err
	}
	if err := rows.Close(); err != nil {
		return s, idx, err
	}
	return s, idx, tx.Commit()
}

// OpenPlaybackRecordingFile validates canonical DB paths against the two managed
// roots. No caller-supplied path is accepted, and only committed bytes are served.
func (d *DB) OpenPlaybackRecordingFile(ctx context.Context, id int64, recordingsDir, tempDir string) (*os.File, error) {
	d.recordingMediaMu.Lock()
	defer d.recordingMediaMu.Unlock()
	s, err := d.GetRecordingSegmentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.Status == "deleted" {
		return nil, ErrPlaybackMediaDeleted
	}
	paths := []struct{ root, path string }{{recordingsDir, s.FinalPath}, {tempDir, s.TempPath}}
	for _, candidate := range paths {
		if candidate.path == "" {
			continue
		}
		path, _, err := safeRecordingSegmentPath(candidate.root, candidate.path)
		if errors.Is(err, ErrRecordingSegmentFileMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("playback file: %w", ErrRecordingSegmentFileMissing)
}
