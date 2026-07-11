package store

import "context"

func (d *DB) ensureCameraProfileSchema(ctx context.Context) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"layout_key", "TEXT NOT NULL DEFAULT ''"},
		{"recording_stream_name", "TEXT NOT NULL DEFAULT ''"},
		{"live_stream_name", "TEXT NOT NULL DEFAULT ''"},
		{"profile_template_id", "INTEGER"},
		{"manufacturer", "TEXT NOT NULL DEFAULT ''"},
		{"model", "TEXT NOT NULL DEFAULT ''"},
		{"profile_adapter", "TEXT NOT NULL DEFAULT ''"},
		{"host", "TEXT NOT NULL DEFAULT ''"},
		{"rtsp_port", "INTEGER NOT NULL DEFAULT 0"},
		{"http_port", "INTEGER NOT NULL DEFAULT 0"},
		{"onvif_port", "INTEGER NOT NULL DEFAULT 0"},
		{"channel_index", "INTEGER"},
		{"last_scan_json", "TEXT NOT NULL DEFAULT '{}'"},
		{"control_capabilities_json", "TEXT NOT NULL DEFAULT '{}'"},
	}
	for _, column := range columns {
		if err := d.addColumnIfMissing(ctx, "cameras", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}
