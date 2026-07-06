package store

import (
	"errors"
	"testing"
	"time"
)

func TestSettings_BackupCronScheduleAndProtectDefaultsPersist(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	legacy := `{"recording":{"segmentMinutes":5,"retentionDays":30,"maxStorageGB":10},"backup":{"enabled":true,"target":"gdrive:/cctvTest","retentionDays":14},"alerts":{}}`
	if _, err := db.db.ExecContext(t.Context(), `INSERT INTO settings(key, value_json, updated_at) VALUES ('console', ?, ?)`, legacy, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed legacy settings: %v", err)
	}

	// When
	read, err := db.GetSettings(t.Context())

	// Then
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if read.Backup.ScheduleCron != "0 3 * * *" {
		t.Fatalf("default schedule cron = %q, want daily 03:00", read.Backup.ScheduleCron)
	}
	if !read.Backup.ProtectUnbacked {
		t.Fatalf("legacy settings should protect unbacked recordings by default")
	}

	// When
	updated, err := db.UpdateSettings(t.Context(), SettingsUpdate{
		Backup: &BackupSettings{
			Enabled:         true,
			Target:          "gdrive:/cctvTest",
			RetentionDays:   14,
			ScheduleEnabled: true,
			ScheduleCron:    "*/15 * * * *",
			ProtectUnbacked: false,
		},
	})

	// Then
	if err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	if !updated.Backup.ScheduleEnabled || updated.Backup.ScheduleCron != "*/15 * * * *" {
		t.Fatalf("schedule settings not persisted: %#v", updated.Backup)
	}
	if updated.Backup.ProtectUnbacked {
		t.Fatalf("explicit protectUnbacked=false should persist")
	}
}

func TestSettings_BackupScheduleCron_rejectsInvalidExpression(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)

	// When
	_, err := db.UpdateSettings(t.Context(), SettingsUpdate{
		Backup: &BackupSettings{
			Enabled:         true,
			Target:          "gdrive:/cctvTest",
			RetentionDays:   14,
			ScheduleEnabled: true,
			ScheduleCron:    "bad cron",
			ProtectUnbacked: true,
		},
	})

	// Then
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("cron validation error = %v, want ErrValidation", err)
	}
}
