package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func createCLIFixture(t *testing.T) (string, []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE cameras (id TEXT PRIMARY KEY,display_name TEXT NOT NULL,location TEXT,enabled INTEGER NOT NULL,archived_at REAL,main_stream_url TEXT NOT NULL,sub_stream_url TEXT,onvif_host TEXT,onvif_port INTEGER,onvif_username TEXT,onvif_password TEXT,sort_order INTEGER NOT NULL,notes TEXT,created_at REAL NOT NULL,updated_at REAL NOT NULL)`,
		`CREATE TABLE layouts (id TEXT PRIMARY KEY,name TEXT NOT NULL,data TEXT NOT NULL,timeline_collapsed INTEGER NOT NULL,grid_cols INTEGER NOT NULL,grid_rows INTEGER,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL)`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY,value TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	username, password, token := "cli-user", "cli-"+"secret", "cli-"+"token"
	rawURL := "rtsp://" + username + ":" + password + "@192.0.2.20/main?token=" + token
	if _, err := db.Exec(`INSERT INTO cameras VALUES('테스트1','테스트',NULL,1,NULL,?,NULL,NULL,NULL,NULL,NULL,0,NULL,1,1)`, rawURL); err != nil {
		t.Fatalf("insert camera: %v", err)
	}
	for key, value := range map[string]string{"retention_days": "30", "segment_minutes": "30", "max_storage_gb": "700", "motion_enabled": "0", "motion_threshold": "0.02"} {
		if _, err := db.Exec(`INSERT INTO settings VALUES(?,?)`, key, value); err != nil {
			t.Fatalf("insert setting: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO layouts VALUES('layout-cli','기본','[{"i":"테스트1","x":0,"y":0,"w":48,"h":48,"minW":8,"minH":8}]',1,48,48,1,1)`); err != nil {
		t.Fatalf("insert layout: %v", err)
	}
	return path, []string{rawURL, username, password, token}
}

func TestRunDryRunEmitsSecretFreeJSON(t *testing.T) {
	t.Parallel()
	source, forbidden := createCLIFixture(t)
	var stdout, stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"dry-run", "-source", source, "-expect-cameras", "1", "-expect-enabled", "1"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q stdout=%q", exitCode, stderr.String(), stdout.String())
	}
	for _, secret := range forbidden {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Fatalf("CLI output leaked synthetic secret")
		}
	}
	if !strings.Contains(stdout.String(), `"operation": "dry-run"`) || !strings.Contains(stdout.String(), `"ready": true`) {
		t.Fatalf("unexpected CLI output: %s", stdout.String())
	}
}

func TestRunRejectsMissingTarget(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"import", "-source", "snapshot.db"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunCameraLayoutRequiresAndUsesExplicitTargetPolicy(t *testing.T) {
	t.Parallel()
	source, forbidden := createCLIFixture(t)
	var missingOut, missingErr bytes.Buffer
	missingCode := run(context.Background(), []string{
		"dry-run", "-source", source, "-scope", "camera-layout",
	}, &missingOut, &missingErr)
	if missingCode != 2 || !strings.Contains(missingOut.String(), `"TARGET_POLICY_REQUIRED"`) {
		t.Fatalf("missing target policy exit=%d stdout=%q stderr=%q", missingCode, missingOut.String(), missingErr.String())
	}

	target := filepath.Join(t.TempDir(), "camera-layout.db")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"import", "-source", source, "-target", target, "-scope", "camera-layout",
		"-expect-cameras", "1", "-expect-enabled", "1", "-expect-layouts", "1", "-expect-layout-items", "1",
		"-target-segment-minutes", "7", "-target-retention-days", "14", "-target-max-storage-gb", "42",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("camera-layout CLI exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, secret := range forbidden {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Fatalf("camera-layout CLI leaked synthetic secret")
		}
	}
	if !strings.Contains(stdout.String(), `"scope": "camera-layout"`) || !strings.Contains(stdout.String(), `"targetStatus": "created"`) ||
		!strings.Contains(stdout.String(), `"segmentMinutes": 7`) || !strings.Contains(stdout.String(), `"retentionDays": 14`) || !strings.Contains(stdout.String(), `"maxStorageGB": 42`) {
		t.Fatalf("unexpected camera-layout CLI manifest: %s", stdout.String())
	}
}

func TestRunSnapshotRequiresTarget(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"snapshot", "-source", "active.db"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunGo2RTCCanaryRequiresSelectionContract(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"go2rtc-canary", "-source", "go2rtc.yaml", "-target", "canary.db",
	}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "selection") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}
