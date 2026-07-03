package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrValidation = errors.New("validation")

type RecordingSettings struct {
	SegmentMinutes int     `json:"segmentMinutes"`
	RetentionDays  int     `json:"retentionDays"`
	MaxStorageGB   float64 `json:"maxStorageGB"`
}

type BackupSettings struct {
	Enabled                 bool   `json:"enabled"`
	Target                  string `json:"target"`
	RetentionDays           int    `json:"retentionDays"`
	ScheduleEnabled         bool   `json:"scheduleEnabled"`
	ScheduleIntervalMinutes int    `json:"scheduleIntervalMinutes"`
	ProtectUnbacked         bool   `json:"protectUnbacked"`
}

type SecretDisplay struct {
	HasSecret   bool   `json:"hasSecret"`
	Masked      string `json:"masked"`
	Fingerprint string `json:"fingerprint"`
}

type AlertSettings struct {
	DiscordEnabled bool          `json:"discordEnabled"`
	DiscordWebhook SecretDisplay `json:"discordWebhook"`
}

type AlertDeliverySettings struct {
	DiscordEnabled    bool
	DiscordWebhookURL string
	DiscordWebhook    SecretDisplay
}

type Settings struct {
	Recording RecordingSettings `json:"recording"`
	Backup    BackupSettings    `json:"backup"`
	Alerts    AlertSettings     `json:"alerts"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type SettingsUpdate struct {
	Recording *RecordingSettings   `json:"recording"`
	Backup    *BackupSettings      `json:"backup"`
	Alerts    *AlertSettingsUpdate `json:"alerts"`
}

type AlertSettingsUpdate struct {
	DiscordEnabled    bool    `json:"discordEnabled"`
	DiscordWebhookURL *string `json:"discordWebhookUrl,omitempty"`
}

type settingsPayload struct {
	Recording RecordingSettings   `json:"recording"`
	Backup    BackupSettings      `json:"backup"`
	Alerts    alertSettingsStored `json:"alerts"`
}

type alertSettingsStored struct {
	DiscordEnabled    bool   `json:"discordEnabled"`
	DiscordWebhookURL string `json:"discordWebhookUrl,omitempty"`
}

func defaultSettingsPayload() settingsPayload {
	return settingsPayload{
		Recording: RecordingSettings{
			SegmentMinutes: 30,
			RetentionDays:  30,
			MaxStorageGB:   0,
		},
		Backup: BackupSettings{
			Enabled:                 false,
			Target:                  "gdrive:/cctvTest",
			RetentionDays:           30,
			ScheduleEnabled:         false,
			ScheduleIntervalMinutes: 1440,
			ProtectUnbacked:         true,
		},
		Alerts: alertSettingsStored{},
	}
}

func (d *DB) GetSettings(ctx context.Context) (Settings, error) {
	payload, updatedAt, err := d.loadSettingsPayload(ctx)
	if err != nil {
		return Settings{}, err
	}
	return publicSettings(payload, updatedAt), nil
}

func (d *DB) GetAlertDeliverySettings(ctx context.Context) (AlertDeliverySettings, error) {
	payload, _, err := d.loadSettingsPayload(ctx)
	if err != nil {
		return AlertDeliverySettings{}, err
	}
	return AlertDeliverySettings{
		DiscordEnabled:    payload.Alerts.DiscordEnabled,
		DiscordWebhookURL: payload.Alerts.DiscordWebhookURL,
		DiscordWebhook:    secretDisplay(payload.Alerts.DiscordWebhookURL),
	}, nil
}

func (d *DB) UpdateSettings(ctx context.Context, update SettingsUpdate) (Settings, error) {
	payload, _, err := d.loadSettingsPayload(ctx)
	if err != nil {
		return Settings{}, err
	}
	if update.Recording != nil {
		if err := validateRecordingSettings(*update.Recording); err != nil {
			return Settings{}, err
		}
		payload.Recording = *update.Recording
	}
	if update.Backup != nil {
		backup := *update.Backup
		if backup.ScheduleIntervalMinutes == 0 {
			backup.ScheduleIntervalMinutes = defaultSettingsPayload().Backup.ScheduleIntervalMinutes
		}
		if err := validateBackupSettings(backup); err != nil {
			return Settings{}, err
		}
		payload.Backup = backup
	}
	if update.Alerts != nil {
		if update.Alerts.DiscordWebhookURL != nil {
			webhook := strings.TrimSpace(*update.Alerts.DiscordWebhookURL)
			if webhook != "" {
				if err := validateDiscordWebhookURL(webhook); err != nil {
					return Settings{}, err
				}
			}
			payload.Alerts.DiscordWebhookURL = webhook
		}
		payload.Alerts.DiscordEnabled = update.Alerts.DiscordEnabled
	}
	updatedAt := time.Now().UTC()
	if err := d.saveSettingsPayload(ctx, payload, updatedAt); err != nil {
		return Settings{}, err
	}
	return publicSettings(payload, updatedAt), nil
}

func (d *DB) ResetSettings(ctx context.Context) (Settings, error) {
	payload := defaultSettingsPayload()
	updatedAt := time.Now().UTC()
	if err := d.saveSettingsPayload(ctx, payload, updatedAt); err != nil {
		return Settings{}, err
	}
	return publicSettings(payload, updatedAt), nil
}

func (d *DB) loadSettingsPayload(ctx context.Context) (settingsPayload, time.Time, error) {
	payload := defaultSettingsPayload()
	var valueJSON, updatedAtText string
	err := d.db.QueryRowContext(ctx, `SELECT value_json, updated_at FROM settings WHERE key = ?`, "console").Scan(&valueJSON, &updatedAtText)
	if errors.Is(err, sql.ErrNoRows) {
		return payload, time.Time{}, nil
	}
	if err != nil {
		return settingsPayload{}, time.Time{}, fmt.Errorf("load settings: %w", err)
	}
	if err := json.Unmarshal([]byte(valueJSON), &payload); err != nil {
		return settingsPayload{}, time.Time{}, fmt.Errorf("decode settings: %w", err)
	}
	payload = normalizeSettingsPayload(payload, valueJSON)
	updatedAt, err := parseOptionalTime(updatedAtText)
	if err != nil {
		return settingsPayload{}, time.Time{}, fmt.Errorf("parse settings updated_at: %w", err)
	}
	return payload, updatedAt, nil
}

func (d *DB) saveSettingsPayload(ctx context.Context, payload settingsPayload, updatedAt time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO settings(key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		"console",
		string(encoded),
		updatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

func publicSettings(payload settingsPayload, updatedAt time.Time) Settings {
	return Settings{
		Recording: payload.Recording,
		Backup:    payload.Backup,
		Alerts: AlertSettings{
			DiscordEnabled: payload.Alerts.DiscordEnabled,
			DiscordWebhook: secretDisplay(payload.Alerts.DiscordWebhookURL),
		},
		UpdatedAt: updatedAt,
	}
}
