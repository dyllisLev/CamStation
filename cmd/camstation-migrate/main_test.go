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
