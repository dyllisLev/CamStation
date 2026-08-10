package legacyimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"camstation/internal/store"
)

func Inspect(ctx context.Context, source string, expectations Expectations) (Manifest, error) {
	prepared, err := buildPlan(ctx, source, expectations)
	prepared.manifest.Operation = "inspect"
	return prepared.manifest, err
}

func DryRun(ctx context.Context, source string, expectations Expectations) (Manifest, error) {
	prepared, err := buildPlan(ctx, source, expectations)
	prepared.manifest.Operation = "dry-run"
	return prepared.manifest, err
}

func Import(ctx context.Context, source, target string, expectations Expectations) (Manifest, error) {
	prepared, err := buildPlan(ctx, source, expectations)
	prepared.manifest.Operation = "import"
	if err != nil || !prepared.manifest.Ready {
		return prepared.manifest, err
	}
	return promotePlan(ctx, source, target, prepared)
}

func promotePlan(ctx context.Context, source, target string, prepared plan) (Manifest, error) {
	if strings.TrimSpace(target) == "" {
		return prepared.manifest, errors.New("target path is required")
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return prepared.manifest, fmt.Errorf("resolve source path: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return prepared.manifest, fmt.Errorf("resolve target path: %w", err)
	}
	if sourceAbs == targetAbs {
		prepared.manifest.Ready = false
		prepared.manifest.TargetStatus = "rejected"
		prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "TARGET_EQUALS_SOURCE", Message: "the 2.0 target must not be the source input"})
		return prepared.manifest, nil
	}

	if info, statErr := os.Lstat(targetAbs); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			prepared.manifest.Ready = false
			prepared.manifest.TargetStatus = "rejected"
			prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "TARGET_NOT_REGULAR", Message: "the existing target is not a regular file"})
			return prepared.manifest, nil
		}
		actual, readErr := readCanonicalTarget(ctx, targetAbs)
		if readErr == nil && canonicalFingerprint(actual) == canonicalFingerprint(prepared.canon) {
			prepared.manifest.TargetStatus = "already-current"
			return prepared.manifest, nil
		}
		prepared.manifest.Ready = false
		prepared.manifest.TargetStatus = "rejected"
		prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "TARGET_EXISTS_DIFFERENT", Message: "an existing target was not overwritten because it does not match the source plan"})
		return prepared.manifest, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return prepared.manifest, fmt.Errorf("inspect target path: %w", statErr)
	}

	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o750); err != nil {
		return prepared.manifest, fmt.Errorf("create target directory: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetAbs), ".camstation-import-*.db")
	if err != nil {
		return prepared.manifest, fmt.Errorf("create staged database: %w", err)
	}
	staged := tempFile.Name()
	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		cleanupSQLiteFiles(staged)
		return prepared.manifest, fmt.Errorf("protect staged database: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		cleanupSQLiteFiles(staged)
		return prepared.manifest, fmt.Errorf("close staged database placeholder: %w", err)
	}
	defer cleanupSQLiteFiles(staged)

	if err := writePlan(ctx, staged, prepared); err != nil {
		return prepared.manifest, err
	}
	actual, err := readCanonicalTarget(ctx, staged)
	if err != nil {
		return prepared.manifest, fmt.Errorf("verify staged database: %w", err)
	}
	if canonicalFingerprint(actual) != canonicalFingerprint(prepared.canon) {
		return prepared.manifest, errors.New("staged database verification did not match the source plan")
	}
	if err := syncFile(staged); err != nil {
		return prepared.manifest, fmt.Errorf("sync staged database: %w", err)
	}
	if err := os.Link(staged, targetAbs); err != nil {
		if errors.Is(err, os.ErrExist) {
			prepared.manifest.Ready = false
			prepared.manifest.TargetStatus = "rejected"
			prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "TARGET_RACE_DETECTED", Message: "the target appeared during import and was not overwritten"})
			return prepared.manifest, nil
		}
		return prepared.manifest, fmt.Errorf("promote staged database without overwrite: %w", err)
	}
	if err := os.Remove(staged); err != nil {
		return prepared.manifest, fmt.Errorf("remove staged database link: %w", err)
	}
	if err := syncDirectory(filepath.Dir(targetAbs)); err != nil {
		return prepared.manifest, fmt.Errorf("sync target directory: %w", err)
	}
	prepared.manifest.TargetStatus = "created"
	return prepared.manifest, nil
}

func Verify(ctx context.Context, source, target string, expectations Expectations) (Manifest, error) {
	prepared, err := buildPlan(ctx, source, expectations)
	prepared.manifest.Operation = "verify"
	if err != nil || !prepared.manifest.Ready {
		return prepared.manifest, err
	}
	actual, err := readCanonicalTarget(ctx, target)
	if err != nil {
		prepared.manifest.Ready = false
		prepared.manifest.TargetStatus = "rejected"
		prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "TARGET_UNREADABLE", Message: "the target could not be read as an inactive 2.0 database"})
		return prepared.manifest, nil
	}
	if canonicalFingerprint(actual) != canonicalFingerprint(prepared.canon) {
		prepared.manifest.Ready = false
		prepared.manifest.TargetStatus = "mismatch"
		prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "TARGET_MISMATCH", Message: "the target does not match the canonical source conversion"})
		return prepared.manifest, nil
	}
	prepared.manifest.TargetStatus = "verified"
	return prepared.manifest, nil
}

func writePlan(ctx context.Context, path string, prepared plan) error {
	db, err := store.Open(path)
	if err != nil {
		return fmt.Errorf("open staged 2.0 database: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			db.Close()
		}
	}()
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate staged 2.0 database: %w", err)
	}
	for _, planned := range prepared.cameras {
		saved, err := db.SaveCameraConfiguration(ctx, planned.row, nil)
		if err != nil {
			return fmt.Errorf("write staged camera %s: %w", planned.row.StreamName, err)
		}
		if err := db.SetCameraEnabled(ctx, saved.StreamName, planned.row.Enabled); err != nil {
			return fmt.Errorf("preserve staged camera enabled state for %s: %w", planned.row.StreamName, err)
		}
	}
	for _, layout := range prepared.layouts {
		if _, err := db.CreateLayout(ctx, layout); err != nil {
			return fmt.Errorf("write staged layout: %w", err)
		}
	}
	if _, err := db.UpdateSettings(ctx, prepared.settings); err != nil {
		return fmt.Errorf("write staged settings: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close staged 2.0 database: %w", err)
	}
	closed = true
	return os.Chmod(path, 0o600)
}

func readCanonicalTarget(ctx context.Context, path string) (canonicalTarget, error) {
	db, err := openLegacyReadOnly(ctx, path)
	if err != nil {
		return canonicalTarget{}, err
	}
	defer db.Close()
	var check string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return canonicalTarget{}, errors.New("target SQLite quick-check failed")
	}
	result := canonicalTarget{}
	type cameraRecord struct {
		id     int64
		camera canonicalCamera
	}
	var records []cameraRecord
	cameraRows, err := db.QueryContext(ctx, `SELECT c.id,c.name,c.stream_name,c.layout_key,c.enabled,c.host,c.rtsp_port,c.http_port,c.onvif_port,
		p.desired_revision,p.applied_revision,p.apply_state
		FROM cameras c JOIN camera_policy_states p ON p.camera_id=c.id ORDER BY c.id`)
	if err != nil {
		return result, err
	}
	for cameraRows.Next() {
		var id int64
		var enabled int
		var camera canonicalCamera
		if err := cameraRows.Scan(&id, &camera.Name, &camera.Key, &camera.LayoutKey, &enabled, &camera.Host, &camera.RTSPPort, &camera.HTTPPort, &camera.ONVIFPort,
			&camera.DesiredRevision, &camera.AppliedRevision, &camera.ApplyState); err != nil {
			cameraRows.Close()
			return result, err
		}
		camera.Enabled = enabled != 0
		records = append(records, cameraRecord{id: id, camera: camera})
	}
	if err := cameraRows.Close(); err != nil {
		return result, err
	}
	for _, record := range records {
		inputs, err := readCanonicalInputs(ctx, db, record.id)
		if err != nil {
			return result, err
		}
		outputs, err := readCanonicalOutputs(ctx, db, record.id)
		if err != nil {
			return result, err
		}
		record.camera.Inputs, record.camera.Outputs = inputs, outputs
		result.Cameras = append(result.Cameras, record.camera)
	}

	layoutRows, err := db.QueryContext(ctx, `SELECT id,name,data,timeline_collapsed,grid_cols,grid_rows FROM layouts ORDER BY id`)
	if err != nil {
		return result, err
	}
	for layoutRows.Next() {
		var layout canonicalLayout
		var data string
		var collapsed int
		var gridRows sql.NullInt64
		if err := layoutRows.Scan(&layout.ID, &layout.Name, &data, &collapsed, &layout.GridCols, &gridRows); err != nil {
			layoutRows.Close()
			return result, err
		}
		if err := json.Unmarshal([]byte(data), &layout.Data); err != nil {
			layoutRows.Close()
			return result, err
		}
		layout.TimelineCollapsed = collapsed != 0
		if gridRows.Valid {
			value := int(gridRows.Int64)
			layout.GridRows = &value
		}
		result.Layouts = append(result.Layouts, layout)
	}
	if err := layoutRows.Close(); err != nil {
		return result, err
	}

	var settingsJSON string
	if err := db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key='console'`).Scan(&settingsJSON); err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(settingsJSON), &result.Settings); err != nil {
		return result, err
	}
	return result, nil
}

func readCanonicalInputs(ctx context.Context, db *sql.DB, cameraID int64) ([]canonicalCameraInput, error) {
	rows, err := db.QueryContext(ctx, `SELECT role,source_key,label,source,url,go2rtc_stream_name FROM camera_streams WHERE camera_id=?
		ORDER BY CASE role WHEN 'recording' THEN 0 WHEN 'live' THEN 1 WHEN 'snapshot' THEN 2 ELSE 3 END,id`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var inputs []canonicalCameraInput
	for rows.Next() {
		var input canonicalCameraInput
		if err := rows.Scan(&input.Role, &input.SourceKey, &input.Label, &input.Source, &input.URL, &input.Go2RTCStreamName); err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, rows.Err()
}

func readCanonicalOutputs(ctx context.Context, db *sql.DB, cameraID int64) ([]canonicalCameraOutput, error) {
	rows, err := db.QueryContext(ctx, `SELECT o.purpose,o.stream_name,s.source_key,o.video_mode,o.max_width,o.max_height,o.max_fps,o.audio_mode,o.activation
		FROM camera_outputs o JOIN camera_streams s ON s.id=o.source_stream_id WHERE o.camera_id=?
		ORDER BY CASE o.purpose WHEN 'recording' THEN 0 WHEN 'live' THEN 1 ELSE 2 END`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var outputs []canonicalCameraOutput
	for rows.Next() {
		var output canonicalCameraOutput
		var width, height sql.NullInt64
		var fps sql.NullFloat64
		if err := rows.Scan(&output.Purpose, &output.StreamName, &output.SourceKey, &output.VideoMode, &width, &height, &fps, &output.AudioMode, &output.Activation); err != nil {
			return nil, err
		}
		if width.Valid {
			value := int(width.Int64)
			output.MaxWidth = &value
		}
		if height.Valid {
			value := int(height.Int64)
			output.MaxHeight = &value
		}
		if fps.Valid {
			value := fps.Float64
			output.MaxFPS = &value
		}
		outputs = append(outputs, output)
	}
	return outputs, rows.Err()
}

func cleanupSQLiteFiles(path string) {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Remove(candidate)
	}
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
