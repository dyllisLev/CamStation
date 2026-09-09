package store

import "context"

func (d *DB) ensurePlaybackSchema(ctx context.Context) error {
	if err := d.addColumnIfMissing(ctx, "recording_segments", "camera_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, statement := range []string{
		`UPDATE recording_segments SET camera_name=COALESCE((SELECT name FROM cameras WHERE cameras.id=recording_segments.camera_id),'') WHERE camera_name=''`,
		`CREATE TABLE IF NOT EXISTS recording_media (
 segment_id INTEGER PRIMARY KEY REFERENCES recording_segments(id),
 init_length INTEGER NOT NULL, committed_offset INTEGER NOT NULL,
 video_codec TEXT NOT NULL, time_basis TEXT NOT NULL
 )`,
		`CREATE TABLE IF NOT EXISTS recording_fragments (
 segment_id INTEGER NOT NULL REFERENCES recording_media(segment_id),
 sequence INTEGER NOT NULL, byte_offset INTEGER NOT NULL, byte_length INTEGER NOT NULL,
 media_start_ms INTEGER NOT NULL, media_end_ms INTEGER NOT NULL,
 start_ms INTEGER NOT NULL, end_ms INTEGER NOT NULL,
 PRIMARY KEY(segment_id,sequence)
 )`,
		`CREATE INDEX IF NOT EXISTS idx_recording_fragments_time ON recording_fragments(segment_id,start_ms,end_ms)`,
	} {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
