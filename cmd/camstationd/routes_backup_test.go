package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"camstation/internal/backup"
	"camstation/internal/store"
)

func TestBackupAPI_ConfigStatusJobRetryAndDeleteHistory(t *testing.T) {
	// Given
	useRouteBackupRunner(t, backup.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) error {
		return nil
	}))
	server := newTestRouteServer(t)
	source := t.TempDir()
	updateBody := `{"enabled":true,"target":"gdrive:/cctvTest","retentionDays":9}`

	// When
	configStatus, config := requestJSON(t, server.handler, http.MethodPut, "/api/backup/config", updateBody)
	createStatus, created := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs", `{"source":"`+source+`","prefix":"qa/api"}`)

	// Then
	if configStatus != http.StatusOK {
		t.Fatalf("PUT config status = %d, want %d; body=%#v", configStatus, http.StatusOK, config)
	}
	if createStatus != http.StatusCreated {
		t.Fatalf("POST backup job status = %d, want %d; body=%#v", createStatus, http.StatusCreated, created)
	}
	id := int64(created["id"].(float64))
	created = waitForRouteJobState(t, server.handler, id, "succeeded")
	if created["state"] != "succeeded" {
		t.Fatalf("created backup state = %#v", created)
	}
	statusCode, status := requestJSON(t, server.handler, http.MethodGet, "/api/backup/status", "")
	if statusCode != http.StatusOK || status["config"] == nil {
		t.Fatalf("status response = %d/%#v", statusCode, status)
	}
	jobsStatus, jobs := requestJSON(t, server.handler, http.MethodGet, "/api/backup/jobs", "")
	if jobsStatus != http.StatusOK {
		t.Fatalf("jobs status = %d, want %d; body=%#v", jobsStatus, http.StatusOK, jobs)
	}
	detailStatus, detail := requestJSON(t, server.handler, http.MethodGet, "/api/backup/jobs/"+strconv.FormatInt(id, 10), "")
	if detailStatus != http.StatusOK || detail["id"] != float64(id) {
		t.Fatalf("detail response = %d/%#v", detailStatus, detail)
	}
	retryStatus, retry := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs/"+strconv.FormatInt(id, 10)+"/retry", `{}`)
	if retryStatus != http.StatusCreated {
		t.Fatalf("retry status = %d, want %d; body=%#v", retryStatus, http.StatusCreated, retry)
	}
	retryID := int64(retry["id"].(float64))
	retry = waitForRouteJobState(t, server.handler, retryID, "succeeded")
	deleteStatus, deleted := requestJSON(t, server.handler, http.MethodDelete, "/api/backup/jobs/"+strconv.FormatInt(id, 10), "")
	if deleteStatus != http.StatusOK || deleted["state"] != "deleted" {
		t.Fatalf("delete response = %d/%#v", deleteStatus, deleted)
	}
	writeAPIEvidence(t, "backup-config.json", map[string]any{"status": configStatus, "body": config})
	writeAPIEvidence(t, "backup-job-create.json", map[string]any{"status": createStatus, "body": created})
	writeAPIEvidence(t, "backup-status.json", map[string]any{"status": statusCode, "body": status})
	writeAPIEvidence(t, "backup-jobs.json", map[string]any{"status": jobsStatus, "body": jobs})
	writeAPIEvidence(t, "backup-job-detail.json", map[string]any{"status": detailStatus, "body": detail})
	writeAPIEvidence(t, "backup-job-retry.json", map[string]any{"status": retryStatus, "body": retry})
	writeAPIEvidence(t, "backup-job-delete.json", map[string]any{"status": deleteStatus, "body": deleted})
}

func TestBackupAPI_InvalidTarget_createsFailedJobWithoutLeak(t *testing.T) {
	// Given
	useRouteBackupRunner(t, backup.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) error {
		return nil
	}))
	server := newTestRouteServer(t)
	source := t.TempDir()
	rawTarget := "../prod"
	configStatus, config := requestJSON(t, server.handler, http.MethodPut, "/api/backup/config", `{"enabled":true,"target":"`+rawTarget+`","retentionDays":3}`)
	if configStatus != http.StatusOK {
		t.Fatalf("config status = %d, want %d; body=%#v", configStatus, http.StatusOK, config)
	}

	// When
	createStatus, failed := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs", `{"source":"`+source+`","prefix":"qa/invalid-target"}`)

	// Then
	if createStatus != http.StatusCreated || failed["state"] != "failed" {
		t.Fatalf("failed backup response = %d/%#v", createStatus, failed)
	}
	encoded := mustMarshalString(t, failed)
	if strings.Contains(encoded, rawTarget) {
		t.Fatalf("failed response leaked raw input: %s", encoded)
	}
	writeAPIEvidence(t, "backup-invalid-target-failure.json", map[string]any{"status": createStatus, "body": failed, "redactionProbe": "pass"})
}

func TestBackupAPI_UnavailableSource_createsFailedJobWithoutLeak(t *testing.T) {
	// Given
	useRouteBackupRunner(t, backup.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) error {
		return nil
	}))
	server := newTestRouteServer(t)

	// When
	createStatus, failed := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs", `{"source":"/definitely/not/camstation/recordings","prefix":"qa/missing-source"}`)

	// Then
	if createStatus != http.StatusCreated || failed["state"] != "failed" {
		t.Fatalf("failed backup response = %d/%#v", createStatus, failed)
	}
	encoded := mustMarshalString(t, failed)
	if strings.Contains(encoded, "qa/missing-source") {
		t.Fatalf("failed response leaked raw prefix: %s", encoded)
	}
	writeAPIEvidence(t, "backup-missing-source-failure.json", map[string]any{"status": createStatus, "body": failed, "redactionProbe": "pass"})
}

func TestBackupAPI_UnsafePrefix_rejectsBeforeJobCreationWithoutEcho(t *testing.T) {
	// Given
	useRouteBackupRunner(t, backup.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) error {
		return nil
	}))
	server := newTestRouteServer(t)
	source := t.TempDir()
	unsafePrefix := ".." + "/bad"

	// When
	status, body := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs", `{"source":"`+source+`","prefix":"`+unsafePrefix+`"}`)

	// Then
	if status != http.StatusBadRequest {
		t.Fatalf("unsafe backup prefix status = %d, want %d", status, http.StatusBadRequest)
	}
	if strings.Contains(mustMarshalString(t, body), unsafePrefix) {
		t.Fatalf("unsafe backup prefix response echoed raw input")
	}
	jobsStatus, jobs := requestJSON(t, server.handler, http.MethodGet, "/api/backup/jobs", "")
	if jobsStatus != http.StatusOK {
		t.Fatalf("backup jobs status = %d, want %d", jobsStatus, http.StatusOK)
	}
	jobList, ok := jobs["jobs"].([]any)
	if !ok || len(jobList) != 0 {
		t.Fatalf("unsafe backup prefix created a job")
	}
	writeAPIEvidence(t, "backup-job-prefix-validation.json", map[string]any{
		"status":            status,
		"jobsStatus":        jobsStatus,
		"jobCount":          len(jobList),
		"rawInputEchoCount": 0,
	})
}

func TestBackupAPI_CancelActiveJob(t *testing.T) {
	// Given
	started := make(chan struct{})
	useRouteBackupRunner(t, backup.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}))
	server := newTestRouteServer(t)
	source := t.TempDir()
	createStatus, created := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs", `{"source":"`+source+`","prefix":"qa/cancel"}`)
	if createStatus != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%#v", createStatus, http.StatusCreated, created)
	}
	<-started
	id := int64(created["id"].(float64))

	// When
	deleteStatus, deleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/backup/jobs/"+strconv.FormatInt(id, 10), "")
	cancelStatus, cancelled := requestJSON(t, server.handler, http.MethodPost, "/api/backup/jobs/"+strconv.FormatInt(id, 10)+"/cancel", `{}`)

	// Then
	if deleteStatus != http.StatusConflict {
		t.Fatalf("active delete response = %d/%#v, want 409", deleteStatus, deleteBody)
	}
	if cancelStatus != http.StatusOK || cancelled["state"] != "cancelled" {
		t.Fatalf("cancel response = %d/%#v", cancelStatus, cancelled)
	}
	writeAPIEvidence(t, "backup-job-delete-active-conflict.json", map[string]any{"status": deleteStatus, "body": deleteBody})
	writeAPIEvidence(t, "backup-job-cancel.json", map[string]any{"status": cancelStatus, "body": cancelled})
}

func waitForRouteJobState(t *testing.T, handler http.Handler, id int64, want string) map[string]any {
	t.Helper()

	target := "/api/backup/jobs/" + strconv.FormatInt(id, 10)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, body := requestJSON(t, handler, http.MethodGet, target, "")
		if status != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body=%#v", target, status, http.StatusOK, body)
		}
		if body["state"] == want {
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, body := requestJSON(t, handler, http.MethodGet, target, "")
	t.Fatalf("job %d final response = %d/%#v, want state %s", id, status, body, want)
	return nil
}

func mustMarshalString(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(encoded)
}

func useRouteBackupRunner(t *testing.T, commands backup.CommandRunner) {
	t.Helper()

	previous := buildBackupRunner
	buildBackupRunner = func(db *store.DB) *backup.Runner {
		return backup.NewRunner(db, backup.WithCommandRunner(commands))
	}
	t.Cleanup(func() {
		buildBackupRunner = previous
	})
}
