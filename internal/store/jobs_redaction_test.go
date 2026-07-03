package store

import (
	"errors"
	"strings"
	"testing"
)

func testDiscordWebhookURLUppercase() string {
	return "HTTPS://DISCORD.COM/API/WEBHOOKS/" + strings.Repeat("3", 18) + "/" + strings.Repeat("C", 64)
}

func TestJobs_CreateJob_rejectsSecretLikeSingleFlightKey(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURLUppercase()

	// When
	job, err := db.CreateJob(t.Context(), JobCreate{
		Kind:            "backup",
		SingleFlightKey: "backup:" + secret,
	})

	// Then
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("create secret-like singleFlightKey error = %v, want ErrValidation", err)
	}
	assertJSONDoesNotContain(t, job, secret)
}

func TestJobs_FailJob_redactsFailureTextAndNeutralKeyResult(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURLUppercase()
	job, err := db.CreateJob(t.Context(), JobCreate{Kind: "backup", SingleFlightKey: "backup:redact-fail"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := db.StartJob(t.Context(), job.ID); err != nil {
		t.Fatalf("start job: %v", err)
	}

	// When
	failed, err := db.FailJob(t.Context(), job.ID, "upstream failed "+secret, map[string]any{
		"detail": []any{"prefix", secret},
	})

	// Then
	if err != nil {
		t.Fatalf("fail job: %v", err)
	}
	assertJSONDoesNotContain(t, failed, secret)
	if strings.Contains(failed.Error, secret) || failed.Error == "" {
		t.Fatalf("failure text was not redacted")
	}
	var persistedError, persistedResult string
	if err := db.db.QueryRowContext(t.Context(), `SELECT error, result_json FROM jobs WHERE id = ?`, job.ID).Scan(&persistedError, &persistedResult); err != nil {
		t.Fatalf("read persisted job: %v", err)
	}
	if strings.Contains(persistedError, secret) || strings.Contains(persistedResult, secret) {
		t.Fatalf("persisted job fields leaked forbidden secret")
	}
	jobs, err := db.ListJobs(t.Context(), 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	assertJSONDoesNotContain(t, jobs, secret)
}

func TestJobs_CancelJob_redactsCancellationReason(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURLUppercase()
	job, err := db.CreateJob(t.Context(), JobCreate{Kind: "diagnostics", SingleFlightKey: "diag:redact-cancel"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	// When
	cancelled, err := db.CancelJob(t.Context(), job.ID, "operator stopped "+secret)

	// Then
	if err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	assertJSONDoesNotContain(t, cancelled, secret)
	if strings.Contains(cancelled.Error, secret) || cancelled.Error == "" {
		t.Fatalf("cancellation reason was not redacted")
	}
	var persistedError string
	if err := db.db.QueryRowContext(t.Context(), `SELECT error FROM jobs WHERE id = ?`, job.ID).Scan(&persistedError); err != nil {
		t.Fatalf("read persisted job error: %v", err)
	}
	if strings.Contains(persistedError, secret) {
		t.Fatalf("persisted cancellation reason leaked forbidden secret")
	}
}

func TestJobs_ResultAndEventDetailKeys_areSanitized(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURLUppercase()
	job, err := db.CreateJob(t.Context(), JobCreate{Kind: "backup", SingleFlightKey: "backup:redact-keys"})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := db.StartJob(t.Context(), job.ID); err != nil {
		t.Fatalf("start job: %v", err)
	}

	// When
	succeeded, err := db.SucceedJob(t.Context(), job.ID, map[string]any{
		secret: "value",
		"safe": map[string]any{
			secret: "nested",
			"kept": "ok",
		},
	})
	if err != nil {
		t.Fatalf("succeed job: %v", err)
	}
	if err := db.appendJobEvent(t.Context(), job.ID, "redaction_probe", "event created", map[string]any{
		secret: "event value",
		"safe": "ok",
	}); err != nil {
		t.Fatalf("append job event: %v", err)
	}
	withEvent, err := db.GetJob(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get job with event: %v", err)
	}

	// Then
	assertJSONDoesNotContain(t, succeeded, secret)
	assertJSONDoesNotContain(t, withEvent, secret)
	if _, ok := succeeded.Result["safe"]; !ok {
		t.Fatalf("safe result key was not preserved")
	}
	var persistedResult string
	if err := db.db.QueryRowContext(t.Context(), `SELECT result_json FROM jobs WHERE id = ?`, job.ID).Scan(&persistedResult); err != nil {
		t.Fatalf("read persisted result: %v", err)
	}
	if strings.Contains(persistedResult, secret) {
		t.Fatalf("persisted result_json leaked forbidden key")
	}
	var persistedDetails string
	if err := db.db.QueryRowContext(t.Context(), `SELECT details_json FROM job_events WHERE job_id = ? AND type = ?`, job.ID, "redaction_probe").Scan(&persistedDetails); err != nil {
		t.Fatalf("read persisted event details: %v", err)
	}
	if strings.Contains(persistedDetails, secret) {
		t.Fatalf("persisted event details leaked forbidden key")
	}
}

func TestJobs_CreateJob_rejectsSecretLikeKind(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	secret := testDiscordWebhookURLUppercase()

	// When
	job, err := db.CreateJob(t.Context(), JobCreate{Kind: secret, SingleFlightKey: "kind:redact"})

	// Then
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("create secret-like kind error = %v, want ErrValidation", err)
	}
	assertJSONDoesNotContain(t, job, secret)
	var count int
	if err := db.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM jobs`).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 0 {
		t.Fatalf("secret-like kind persisted %d jobs, want 0", count)
	}
}
