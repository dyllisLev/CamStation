package store

import "context"

// IsRegisteredPublicStream matches the effective names returned by ListCameras
// without hydrating every camera, input stream, output policy, and revision.
func (d *DB) IsRegisteredPublicStream(ctx context.Context, streamName string) (bool, error) {
	if streamName == "" {
		return false, nil
	}
	var registered bool
	err := d.db.QueryRowContext(ctx, `WITH outputs AS (
		SELECT o.camera_id,o.purpose,o.stream_name
		FROM camera_outputs o JOIN camera_streams s ON s.id=o.source_stream_id
	)
	SELECT EXISTS(SELECT 1 FROM cameras c WHERE c.enabled!=0 AND (
		c.stream_name=? OR
		EXISTS(SELECT 1 FROM outputs o WHERE o.camera_id=c.id AND o.stream_name=?) OR
		COALESCE((SELECT o.stream_name FROM outputs o WHERE o.camera_id=c.id AND o.purpose='recording'),NULLIF(c.recording_stream_name,''),c.stream_name)=? OR
		COALESCE((SELECT o.stream_name FROM outputs o WHERE o.camera_id=c.id AND o.purpose='live'),NULLIF(c.live_stream_name,''),c.stream_name)=?
	))`, streamName, streamName, streamName, streamName).Scan(&registered)
	return registered, err
}
