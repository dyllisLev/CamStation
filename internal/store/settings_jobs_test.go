package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSettingsJobTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return db
}

func testDiscordWebhookURL() string {
	return "https://" + "discord.com" + "/api/" + "webhooks/" + strings.Repeat("1", 18) + "/" + strings.Repeat("a", 64)
}

func assertJSONDoesNotContain(t *testing.T, value any, forbidden string) {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if strings.Contains(string(encoded), forbidden) {
		t.Fatalf("public JSON leaked forbidden secret")
	}
}

func TestSettings_CreateReadUpdateReset_masksDiscordSecret(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURL()

	// When
	updated, err := db.UpdateSettings(t.Context(), SettingsUpdate{
		Recording: &RecordingSettings{
			SegmentMinutes: 10,
			RetentionDays:  21,
			MaxStorageGB:   512.5,
		},
		Backup: &BackupSettings{
			Enabled:       true,
			Target:        "gdrive:/cctvTest",
			RetentionDays: 14,
		},
		Alerts: &AlertSettingsUpdate{
			DiscordEnabled:    true,
			DiscordWebhookURL: &secret,
		},
	})

	// Then
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if updated.Recording.RetentionDays != 21 || updated.Backup.Target != "gdrive:/cctvTest" {
		t.Fatalf("updated settings = %#v", updated)
	}
	if !updated.Alerts.DiscordWebhook.HasSecret {
		t.Fatalf("discord secret flag = false, want true")
	}
	if updated.Alerts.DiscordWebhook.Masked == "" || updated.Alerts.DiscordWebhook.Fingerprint == "" {
		t.Fatalf("masked discord secret incomplete: %#v", updated.Alerts.DiscordWebhook)
	}
	assertJSONDoesNotContain(t, updated, secret)

	read, err := db.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if read.Alerts.DiscordWebhook.Fingerprint != updated.Alerts.DiscordWebhook.Fingerprint {
		t.Fatalf("fingerprint changed between read and update")
	}
	assertJSONDoesNotContain(t, read, secret)

	reset, err := db.ResetSettings(t.Context())
	if err != nil {
		t.Fatalf("reset settings: %v", err)
	}
	if reset.Alerts.DiscordWebhook.HasSecret {
		t.Fatalf("reset kept discord secret")
	}
	if reset.Recording.RetentionDays < 0 || reset.Backup.RetentionDays < 0 {
		t.Fatalf("reset settings contain negative retention: %#v", reset)
	}
}

func TestSettings_Update_rejectsInvalidWebhookAndNegativeRetention(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	badWebhook := "https://example.test/not-discord"

	// When
	_, webhookErr := db.UpdateSettings(t.Context(), SettingsUpdate{
		Alerts: &AlertSettingsUpdate{DiscordWebhookURL: &badWebhook},
	})
	_, retentionErr := db.UpdateSettings(t.Context(), SettingsUpdate{
		Recording: &RecordingSettings{SegmentMinutes: 10, RetentionDays: -1, MaxStorageGB: 1},
	})

	// Then
	if !errors.Is(webhookErr, ErrValidation) {
		t.Fatalf("webhook error = %v, want ErrValidation", webhookErr)
	}
	if !errors.Is(retentionErr, ErrValidation) {
		t.Fatalf("retention error = %v, want ErrValidation", retentionErr)
	}
}

func TestJobs_StateTransitionsDuplicateActiveAndRedactedResults(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURL()
	first, err := db.CreateJob(t.Context(), JobCreate{
		Kind:            "backup",
		SingleFlightKey: "backup:daily",
		TimeoutSeconds:  30,
	})
	if err != nil {
		t.Fatalf("create first job: %v", err)
	}

	// When
	_, duplicateErr := db.CreateJob(t.Context(), JobCreate{
		Kind:            "backup",
		SingleFlightKey: "backup:daily",
		TimeoutSeconds:  30,
	})

	// Then
	if !errors.Is(duplicateErr, ErrJobAlreadyActive) {
		t.Fatalf("duplicate error = %v, want ErrJobAlreadyActive", duplicateErr)
	}
	if first.State != JobStateQueued {
		t.Fatalf("created job state = %s, want queued", first.State)
	}

	running, err := db.StartJob(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("start job: %v", err)
	}
	if running.State != JobStateRunning || running.StartedAt == nil {
		t.Fatalf("running job = %#v", running)
	}

	succeeded, err := db.SucceedJob(t.Context(), first.ID, map[string]any{
		"discordWebhookUrl": secret,
		"status":            "ok",
	})
	if err != nil {
		t.Fatalf("succeed job: %v", err)
	}
	if succeeded.State != JobStateSucceeded || succeeded.CompletedAt == nil {
		t.Fatalf("succeeded job = %#v", succeeded)
	}
	if len(succeeded.Events) < 3 {
		t.Fatalf("succeeded job events = %d, want queued/running/succeeded audit events", len(succeeded.Events))
	}
	assertJSONDoesNotContain(t, succeeded, secret)

	second, err := db.CreateJob(t.Context(), JobCreate{
		Kind:            "backup",
		SingleFlightKey: "backup:daily",
	})
	if err != nil {
		t.Fatalf("create second job after completion: %v", err)
	}
	failed, err := db.FailJob(t.Context(), second.ID, "upstream failed", map[string]any{"webhook": secret})
	if err != nil {
		t.Fatalf("fail job: %v", err)
	}
	if failed.State != JobStateFailed || failed.Error == "" {
		t.Fatalf("failed job = %#v", failed)
	}
	assertJSONDoesNotContain(t, failed, secret)

	timeoutJob, err := db.CreateJob(t.Context(), JobCreate{Kind: "diagnostics", SingleFlightKey: "diag:timeout", TimeoutSeconds: 1})
	if err != nil {
		t.Fatalf("create timeout job: %v", err)
	}
	if _, err := db.StartJob(t.Context(), timeoutJob.ID); err != nil {
		t.Fatalf("start timeout job: %v", err)
	}
	timedOut, err := db.TimeoutJob(t.Context(), timeoutJob.ID)
	if err != nil {
		t.Fatalf("timeout job: %v", err)
	}
	if timedOut.State != JobStateFailed || !strings.Contains(timedOut.Error, "timed out") {
		t.Fatalf("timed out job = %#v", timedOut)
	}

	cancelJob, err := db.CreateJob(t.Context(), JobCreate{Kind: "diagnostics", SingleFlightKey: "diag"})
	if err != nil {
		t.Fatalf("create cancel job: %v", err)
	}
	cancelled, err := db.CancelJob(t.Context(), cancelJob.ID, "operator cancelled")
	if err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	if cancelled.State != JobStateCancelled || cancelled.CompletedAt == nil {
		t.Fatalf("cancelled job = %#v", cancelled)
	}

	deleted, err := db.DeleteJob(t.Context(), cancelJob.ID)
	if err != nil {
		t.Fatalf("delete job: %v", err)
	}
	if deleted.State != JobStateDeleted {
		t.Fatalf("deleted job state = %s, want deleted", deleted.State)
	}
}

func TestJobs_RecoverStaleRunningJobs_marksCrashRecoveredFailure(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	job, err := db.CreateJob(t.Context(), JobCreate{Kind: "backup", SingleFlightKey: "backup:stale"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := db.StartJob(t.Context(), job.ID); err != nil {
		t.Fatalf("start job: %v", err)
	}
	staleTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if _, err := db.db.ExecContext(t.Context(), `UPDATE jobs SET updated_at = ? WHERE id = ?`, staleTime, job.ID); err != nil {
		t.Fatalf("age job: %v", err)
	}

	// When
	count, err := db.RecoverStaleRunningJobs(t.Context(), time.Now().UTC().Add(-time.Hour))

	// Then
	if err != nil {
		t.Fatalf("recover stale jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("recovered count = %d, want 1", count)
	}
	recovered, err := db.GetJob(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if recovered.State != JobStateFailed || recovered.CompletedAt == nil {
		t.Fatalf("recovered job = %#v", recovered)
	}
	if len(recovered.Events) < 2 {
		t.Fatalf("recovered job events = %d, want audit event for recovery", len(recovered.Events))
	}
	if !strings.Contains(recovered.Error, "stale running job") {
		t.Fatalf("recovered job error = %q", recovered.Error)
	}
}
