package store

import (
	"context"
	"fmt"
)

const playbackLookupMigration = 20260911

// Keep this predicate identical in the partial indexes and their read queries.
// Unpublished and non-ready legacy files have NULL playback timestamps.
const playbackAvailable = `playback_start_ms IS NOT NULL AND playback_end_ms IS NOT NULL
 AND status IN ('ready','recording','finalizing','failed')`

func (d *DB) ensurePlaybackLookupSchema(ctx context.Context) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applied bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=?)`, playbackLookupMigration).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return tx.Commit()
	}

	// The backfill is the only archive-wide fragment aggregation. Installing the
	// columns, data, indexes, triggers and version in one transaction makes both
	// interruption and a later Migrate safe without repeating it on every startup.
	statements := []string{
		`ALTER TABLE recording_segments ADD COLUMN playback_start_ms INTEGER`,
		`ALTER TABLE recording_segments ADD COLUMN playback_end_ms INTEGER`,
		`ALTER TABLE recording_segments ADD COLUMN playback_fragmented INTEGER NOT NULL DEFAULT 0`,
		`UPDATE recording_segments AS s SET
 playback_fragmented=EXISTS(SELECT 1 FROM recording_media WHERE segment_id=s.id),
 playback_start_ms=CASE WHEN EXISTS(SELECT 1 FROM recording_media WHERE segment_id=s.id)
  THEN (SELECT MIN(start_ms) FROM recording_fragments WHERE segment_id=s.id)
  WHEN status='ready' AND ts_end>ts_start THEN CAST(ROUND(ts_start*1000) AS INTEGER) END,
 playback_end_ms=CASE WHEN EXISTS(SELECT 1 FROM recording_media WHERE segment_id=s.id)
  THEN (SELECT MAX(end_ms) FROM recording_fragments WHERE segment_id=s.id)
  WHEN status='ready' AND ts_end>ts_start THEN CAST(ROUND(ts_end*1000) AS INTEGER) END`,
		`CREATE INDEX idx_recording_playback_start ON recording_segments(camera_id,playback_start_ms,id) WHERE ` + playbackAvailable,
		`CREATE INDEX idx_recording_playback_end ON recording_segments(camera_id,playback_end_ms,id) WHERE ` + playbackAvailable,
		`CREATE INDEX idx_recording_camera_active ON recording_segments(camera_id) WHERE status IN ('recording','finalizing')`,
		`CREATE INDEX idx_recording_camera_archive ON recording_segments(camera_id) WHERE status!='deleted'`,
		`CREATE INDEX idx_recording_stream_archive ON recording_segments(stream_name,camera_id) WHERE status!='deleted'`,
		`CREATE VIRTUAL TABLE recording_playback_ranges USING rtree(segment_id,camera_min,camera_max,start_ms,end_ms)`,
		`INSERT INTO recording_playback_ranges SELECT id,camera_id,camera_id,playback_start_ms,playback_end_ms
 FROM recording_segments WHERE ` + playbackAvailable,
		`CREATE TRIGGER recording_playback_legacy_insert AFTER INSERT ON recording_segments
 WHEN NEW.playback_fragmented=0 BEGIN ` + playbackLegacyUpdate + ` END`,
		`CREATE TRIGGER recording_playback_legacy_update AFTER UPDATE OF ts_start,ts_end,status ON recording_segments
 WHEN NEW.playback_fragmented=0 AND (OLD.ts_start IS NOT NEW.ts_start OR OLD.ts_end IS NOT NEW.ts_end OR OLD.status IS NOT NEW.status)
 BEGIN ` + playbackLegacyUpdate + ` END`,
		`CREATE TRIGGER recording_playback_media_insert AFTER INSERT ON recording_media BEGIN
 UPDATE recording_segments SET playback_fragmented=1,playback_start_ms=NULL,playback_end_ms=NULL WHERE id=NEW.segment_id;
 END`,
		`CREATE TRIGGER recording_playback_fragment_insert AFTER INSERT ON recording_fragments BEGIN
 UPDATE recording_segments SET
 playback_start_ms=MIN(COALESCE(playback_start_ms,NEW.start_ms),NEW.start_ms),
 playback_end_ms=MAX(COALESCE(playback_end_ms,NEW.end_ms),NEW.end_ms)
 WHERE id=NEW.segment_id AND (playback_start_ms IS NULL OR playback_end_ms IS NULL
  OR NEW.start_ms<playback_start_ms OR NEW.end_ms>playback_end_ms);
 END`,
		`CREATE TRIGGER recording_playback_range_insert AFTER INSERT ON recording_segments BEGIN ` + playbackRangeUpdate + ` END`,
		`CREATE TRIGGER recording_playback_range_update AFTER UPDATE OF camera_id,status,playback_start_ms,playback_end_ms ON recording_segments
 WHEN OLD.camera_id IS NOT NEW.camera_id OR OLD.status IS NOT NEW.status
  OR OLD.playback_start_ms IS NOT NEW.playback_start_ms OR OLD.playback_end_ms IS NOT NEW.playback_end_ms
 BEGIN ` + playbackRangeUpdate + ` END`,
		`CREATE TRIGGER recording_playback_range_delete AFTER DELETE ON recording_segments BEGIN
 DELETE FROM recording_playback_ranges WHERE segment_id=OLD.id;
 END`,
		`INSERT INTO schema_migrations(version,applied_at) VALUES (20260911,datetime('now'))`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate playback lookups: %w", err)
		}
	}
	return tx.Commit()
}

// Fragment publication and its exact extrema commit together, including when
// an older application writes the database after a code rollback. Fragment
// rows are immutable; normal deletion changes the parent status, not its index.
const playbackLegacyUpdate = `UPDATE recording_segments SET
 playback_start_ms=CASE WHEN NEW.status='ready' AND NEW.ts_end>NEW.ts_start THEN CAST(ROUND(NEW.ts_start*1000) AS INTEGER) END,
 playback_end_ms=CASE WHEN NEW.status='ready' AND NEW.ts_end>NEW.ts_start THEN CAST(ROUND(NEW.ts_end*1000) AS INTEGER) END
 WHERE id=NEW.id;`

// R-tree coordinates are approximate, outward-rounded bounds. Queries must
// still check exact camera IDs and integer timestamps on recording_segments.
const playbackRangeUpdate = `DELETE FROM recording_playback_ranges WHERE segment_id=NEW.id;
 INSERT INTO recording_playback_ranges SELECT NEW.id,NEW.camera_id,NEW.camera_id,NEW.playback_start_ms,NEW.playback_end_ms
 WHERE NEW.playback_start_ms IS NOT NULL AND NEW.playback_end_ms IS NOT NULL
 AND NEW.status IN ('ready','recording','finalizing','failed');`
