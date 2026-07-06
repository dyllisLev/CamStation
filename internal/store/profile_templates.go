package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (d *DB) CreateCameraProfileTemplate(ctx context.Context, template CameraProfileTemplate) (CameraProfileTemplate, error) {
	normalized, err := normalizeCameraProfileTemplate(template)
	if err != nil {
		return CameraProfileTemplate{}, err
	}
	payload, err := marshalCameraProfileTemplatePayload(normalized)
	if err != nil {
		return CameraProfileTemplate{}, err
	}
	now := time.Now().UTC()
	result, err := d.db.ExecContext(ctx,
		`INSERT INTO camera_profile_templates(
			profile_name, normalized_profile_name, manufacturer, normalized_manufacturer, model, normalized_model,
			adapter, normalized_adapter, version, match_rules_json, channels_json, capabilities_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.ProfileName,
		normalizeProfileTemplateKey(normalized.ProfileName),
		normalized.Manufacturer,
		normalizeProfileTemplateKey(normalized.Manufacturer),
		normalized.Model,
		normalizeProfileTemplateKey(normalized.Model),
		normalized.Adapter,
		normalizeProfileTemplateKey(normalized.Adapter),
		normalized.Version,
		payload.matchRules,
		payload.channels,
		payload.capabilities,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return CameraProfileTemplate{}, profileTemplateWriteError("create camera profile template", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return CameraProfileTemplate{}, fmt.Errorf("create camera profile template id: %w", err)
	}
	return d.GetCameraProfileTemplate(ctx, id)
}

func (d *DB) ListCameraProfileTemplates(ctx context.Context) ([]CameraProfileTemplate, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, profile_name, manufacturer, model, adapter, version, match_rules_json, channels_json,
		        capabilities_json, created_at, updated_at
		 FROM camera_profile_templates
		 ORDER BY normalized_manufacturer, normalized_model, normalized_profile_name, version, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list camera profile templates: %w", err)
	}
	defer rows.Close()

	templates := make([]CameraProfileTemplate, 0)
	for rows.Next() {
		template, err := scanCameraProfileTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list camera profile templates rows: %w", err)
	}
	return templates, nil
}

func (d *DB) GetCameraProfileTemplate(ctx context.Context, id int64) (CameraProfileTemplate, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, profile_name, manufacturer, model, adapter, version, match_rules_json, channels_json,
		        capabilities_json, created_at, updated_at
		 FROM camera_profile_templates
		 WHERE id = ?`,
		id,
	)
	template, err := scanCameraProfileTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return CameraProfileTemplate{}, fmt.Errorf("camera profile template %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return CameraProfileTemplate{}, err
	}
	return template, nil
}

func (d *DB) UpdateCameraProfileTemplate(ctx context.Context, id int64, template CameraProfileTemplate) (CameraProfileTemplate, error) {
	normalized, err := normalizeCameraProfileTemplate(template)
	if err != nil {
		return CameraProfileTemplate{}, err
	}
	payload, err := marshalCameraProfileTemplatePayload(normalized)
	if err != nil {
		return CameraProfileTemplate{}, err
	}
	now := time.Now().UTC()
	result, err := d.db.ExecContext(ctx,
		`UPDATE camera_profile_templates
		 SET profile_name = ?, normalized_profile_name = ?, manufacturer = ?, normalized_manufacturer = ?,
		     model = ?, normalized_model = ?, adapter = ?, normalized_adapter = ?, version = ?,
		     match_rules_json = ?, channels_json = ?, capabilities_json = ?, updated_at = ?
		 WHERE id = ?`,
		normalized.ProfileName,
		normalizeProfileTemplateKey(normalized.ProfileName),
		normalized.Manufacturer,
		normalizeProfileTemplateKey(normalized.Manufacturer),
		normalized.Model,
		normalizeProfileTemplateKey(normalized.Model),
		normalized.Adapter,
		normalizeProfileTemplateKey(normalized.Adapter),
		normalized.Version,
		payload.matchRules,
		payload.channels,
		payload.capabilities,
		now.Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return CameraProfileTemplate{}, profileTemplateWriteError("update camera profile template", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return CameraProfileTemplate{}, fmt.Errorf("update camera profile template rows: %w", err)
	}
	if affected == 0 {
		return CameraProfileTemplate{}, fmt.Errorf("camera profile template %d: %w", id, ErrNotFound)
	}
	return d.GetCameraProfileTemplate(ctx, id)
}

func (d *DB) DeleteCameraProfileTemplate(ctx context.Context, id int64) error {
	var references int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cameras WHERE profile_template_id = ?`, id).Scan(&references); err != nil {
		return fmt.Errorf("count camera profile template references: %w", err)
	}
	if references > 0 {
		return fmt.Errorf("camera profile template %d referenced by %d camera(s): %w", id, references, ErrProfileTemplateInUse)
	}
	result, err := d.db.ExecContext(ctx, `DELETE FROM camera_profile_templates WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete camera profile template: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete camera profile template rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("camera profile template %d: %w", id, ErrNotFound)
	}
	return nil
}
