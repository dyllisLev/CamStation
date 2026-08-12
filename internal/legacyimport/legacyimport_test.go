package legacyimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/store"
)

var productionShapeKeys = []string{
	"집-마당", "집-창고1", "집-창고2", "소방서1", "소방서2", "소방서3", "소방서4", "염소장", "소방서5",
}

type fixtureSecrets struct {
	mainURL  string
	subURL   string
	username string
	password string
	token    string
}

func createLegacyFixture(t *testing.T, mutate func(*sql.DB)) (string, fixtureSecrets) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE cameras (
			id TEXT PRIMARY KEY, display_name TEXT NOT NULL, location TEXT, enabled INTEGER NOT NULL DEFAULT 1,
			archived_at REAL, main_stream_url TEXT NOT NULL, sub_stream_url TEXT, onvif_host TEXT,
			onvif_port INTEGER, onvif_username TEXT, onvif_password TEXT, sort_order INTEGER NOT NULL DEFAULT 0,
			notes TEXT, created_at REAL NOT NULL, updated_at REAL NOT NULL
		)`,
		`CREATE TABLE layouts (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, data TEXT NOT NULL, timeline_collapsed INTEGER NOT NULL DEFAULT 0,
			grid_cols INTEGER NOT NULL DEFAULT 12, grid_rows INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create fixture schema: %v", err)
		}
	}
	secrets := fixtureSecrets{
		username: "migration-user",
		password: "migration-" + "secret",
		token:    "synthetic-" + "token",
	}
	secrets.mainURL = "rtsp://" + secrets.username + ":" + secrets.password + "@192.0.2.10:8554/main?token=" + secrets.token
	secrets.subURL = "rtsp://" + secrets.username + ":" + secrets.password + "@192.0.2.10:8554/sub?token=" + secrets.token
	for index, key := range productionShapeKeys {
		enabled := 1
		if key == "소방서2" {
			enabled = 0
		}
		username, password := secrets.username, secrets.password
		if index == 0 {
			username, password = "separate-onvif", "separate-control-secret"
		}
		_, err := db.Exec(`INSERT INTO cameras(
			id,display_name,location,enabled,archived_at,main_stream_url,sub_stream_url,onvif_host,onvif_port,
			onvif_username,onvif_password,sort_order,notes,created_at,updated_at
		) VALUES(?,?,?,?,NULL,?,?,?,?,?,?,?,?,1000,1000)`,
			key, key+" 카메라", "테스트 위치", enabled, secrets.mainURL+fmt.Sprintf("&camera=%d", index),
			secrets.subURL+fmt.Sprintf("&camera=%d", index), "192.0.2.10", 80, username, password, index, "합성 메모")
		if err != nil {
			t.Fatalf("insert fixture camera: %v", err)
		}
	}
	items := make([]store.LayoutItem, 0, 8)
	enabledKeys := make([]string, 0, 8)
	for _, key := range productionShapeKeys {
		if key != "소방서2" {
			enabledKeys = append(enabledKeys, key)
		}
	}
	for index, key := range enabledKeys {
		items = append(items, store.LayoutItem{I: key, X: (index % 4) * 12, Y: (index / 4) * 24, W: 12, H: 24, MinW: 6, MinH: 12})
	}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal fixture layout: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO layouts(id,name,data,timeline_collapsed,grid_cols,grid_rows,created_at,updated_at)
		VALUES('layout-production','기본',?,0,48,48,1000,1000)`, string(data)); err != nil {
		t.Fatalf("insert fixture layout: %v", err)
	}
	settings := map[string]string{
		"retention_days": "30", "segment_minutes": "30", "motion_threshold": "0.02",
		"max_storage_gb": "700", "motion_enabled": "0",
	}
	for key, value := range settings {
		if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, key, value); err != nil {
			t.Fatalf("insert fixture setting: %v", err)
		}
	}
	if mutate != nil {
		mutate(db)
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint fixture: %v", err)
	}
	return path, secrets
}

func productionExpectations() Expectations {
	return Expectations{
		CameraCount: 9, EnabledCount: 8, SubStreamCount: 9, DisabledCamera: "소방서2",
		LayoutCount: 1, LayoutItemCount: 8, SegmentMinutes: 30, RetentionDays: 30, MaxStorageGB: 700,
	}
}

func TestDryRunPreservesProductionShapeWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()
	source, secrets := createLegacyFixture(t, nil)

	manifest, err := DryRun(t.Context(), source, productionExpectations())
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !manifest.Ready || manifest.QuickCheck != "ok" || !manifest.SchemaCompatible {
		t.Fatalf("manifest not ready: %#v", manifest.Blockers)
	}
	if manifest.Summary.CameraCount != 9 || manifest.Summary.EnabledCount != 8 || manifest.Summary.DisabledCount != 1 || manifest.Summary.SubStreamCount != 9 {
		t.Fatalf("unexpected camera summary: %#v", manifest.Summary)
	}
	if manifest.Summary.LayoutCount != 1 || manifest.Summary.LayoutItemCount != 8 || manifest.Summary.DeferredControl != 1 {
		t.Fatalf("unexpected migration summary: %#v", manifest.Summary)
	}
	if manifest.Settings.SegmentMinutes != 30 || manifest.Settings.RetentionDays != 30 || manifest.Settings.MaxStorageGB != 700 {
		t.Fatalf("unexpected settings: %#v", manifest.Settings)
	}
	if manifest.Settings.BackupEnabled || manifest.Settings.BackupTargetPresent || !manifest.Settings.ProtectUnbacked {
		t.Fatalf("unsafe backup settings: %#v", manifest.Settings)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	for _, forbidden := range []string{secrets.mainURL, secrets.subURL, secrets.username, secrets.password, secrets.token, "separate-control-secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("manifest leaked synthetic secret")
		}
	}
}

func TestImportVerifyAndRepeatAreDeterministic(t *testing.T) {
	t.Parallel()
	source, secrets := createLegacyFixture(t, nil)
	target := filepath.Join(t.TempDir(), "camstation-2.db")

	created, err := Import(t.Context(), source, target, productionExpectations())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !created.Ready || created.TargetStatus != "created" {
		t.Fatalf("created manifest = ready:%v target:%q blockers:%#v", created.Ready, created.TargetStatus, created.Blockers)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %o, want 600", info.Mode().Perm())
	}

	repeated, err := Import(t.Context(), source, target, productionExpectations())
	if err != nil {
		t.Fatalf("repeat import: %v", err)
	}
	if !repeated.Ready || repeated.TargetStatus != "already-current" || repeated.CanonicalFingerprint != created.CanonicalFingerprint {
		t.Fatalf("repeat import = %#v", repeated)
	}
	verified, err := Verify(t.Context(), source, target, productionExpectations())
	if err != nil || !verified.Ready || verified.TargetStatus != "verified" {
		t.Fatalf("verify = %#v err=%v", verified, err)
	}

	db, err := store.Open(target)
	if err != nil {
		t.Fatalf("open target store: %v", err)
	}
	defer db.Close()
	cameras, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatalf("list imported cameras: %v", err)
	}
	if len(cameras) != 9 {
		t.Fatalf("imported cameras = %d, want 9", len(cameras))
	}
	disabled := 0
	for _, camera := range cameras {
		if !camera.Enabled {
			disabled++
			if camera.StreamName != "소방서2" {
				t.Fatalf("unexpected disabled camera: %s", camera.StreamName)
			}
		}
		if len(camera.Streams) != 2 || len(camera.Outputs) != 3 || camera.Outputs[1].SourceKey != "live" {
			t.Fatalf("camera graph not preserved for %s", camera.StreamName)
		}
		if !strings.Contains(camera.Streams[0].URL, secrets.token) {
			t.Fatalf("private target lost recording URL for %s", camera.StreamName)
		}
	}
	if disabled != 1 {
		t.Fatalf("disabled camera count = %d, want 1", disabled)
	}
	settings, err := db.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("get target settings: %v", err)
	}
	if settings.Recording.SegmentMinutes != 30 || settings.Recording.MaxStorageGB != 700 || settings.Backup.Target != "" || !settings.Backup.ProtectUnbacked {
		t.Fatalf("target settings = %#v", settings)
	}
}

func TestBlockedPlanNeverPromotesPartialTarget(t *testing.T) {
	t.Parallel()
	source, _ := createLegacyFixture(t, func(db *sql.DB) {
		bad := `[ {"i":"없는-카메라","x":0,"y":0,"w":12,"h":12} ]`
		if _, err := db.Exec(`UPDATE layouts SET data=?`, bad); err != nil {
			t.Fatalf("corrupt fixture layout: %v", err)
		}
	})
	target := filepath.Join(t.TempDir(), "must-not-exist.db")

	manifest, err := Import(t.Context(), source, target, productionExpectations())
	if err != nil {
		t.Fatalf("blocked import returned infrastructure error: %v", err)
	}
	if manifest.Ready {
		t.Fatalf("blocked import unexpectedly ready")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("blocked import promoted a target: %v", err)
	}
}

func TestImportNeverOverwritesDifferentExistingTarget(t *testing.T) {
	t.Parallel()
	source, _ := createLegacyFixture(t, nil)
	target := filepath.Join(t.TempDir(), "target.db")
	if err := os.WriteFile(target, []byte("operator-owned"), 0o600); err != nil {
		t.Fatalf("seed existing target: %v", err)
	}

	manifest, err := Import(context.Background(), source, target, productionExpectations())
	if err != nil {
		t.Fatalf("existing target result: %v", err)
	}
	if manifest.Ready || manifest.TargetStatus != "rejected" {
		t.Fatalf("existing target was not rejected: %#v", manifest)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "operator-owned" {
		t.Fatalf("existing target was modified")
	}
}

func TestSnapshotUsesOnlineBackupAndNeverOverwrites(t *testing.T) {
	t.Parallel()
	source, secrets := createLegacyFixture(t, nil)
	target := filepath.Join(t.TempDir(), "immutable-snapshot.db")

	manifest, err := Snapshot(t.Context(), source, target, productionExpectations())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !manifest.Ready || manifest.TargetStatus != "created" || manifest.Summary.CameraCount != 9 {
		t.Fatalf("snapshot manifest: %#v", manifest)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode: info=%v err=%v", info, err)
	}
	inspected, err := Inspect(t.Context(), target, productionExpectations())
	if err != nil || !inspected.Ready || inspected.CanonicalFingerprint != manifest.CanonicalFingerprint {
		t.Fatalf("inspect snapshot = %#v err=%v", inspected, err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal snapshot manifest: %v", err)
	}
	for _, forbidden := range []string{secrets.mainURL, secrets.username, secrets.password, secrets.token} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot manifest leaked a synthetic secret")
		}
	}

	repeated, err := Snapshot(t.Context(), source, target, productionExpectations())
	if err != nil {
		t.Fatalf("repeat snapshot: %v", err)
	}
	if repeated.Ready || repeated.TargetStatus != "rejected" {
		t.Fatalf("repeat snapshot did not refuse overwrite: %#v", repeated)
	}
}

func TestDryRunAcceptsSupportedHTTPFLVInputAndRejectsInvalidPort(t *testing.T) {
	t.Parallel()
	httpSource, _ := createLegacyFixture(t, func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE cameras SET main_stream_url='http://192.0.2.30/flv?port=1935' WHERE id='집-마당'`); err != nil {
			t.Fatalf("set HTTP-FLV fixture: %v", err)
		}
	})
	manifest, err := DryRun(t.Context(), httpSource, productionExpectations())
	if err != nil || !manifest.Ready {
		t.Fatalf("HTTP-FLV dry-run = ready:%v blockers:%#v err=%v", manifest.Ready, manifest.Blockers, err)
	}

	badSource, _ := createLegacyFixture(t, func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE cameras SET main_stream_url='rtsp://192.0.2.30:70000/main' WHERE id='집-마당'`); err != nil {
			t.Fatalf("set invalid-port fixture: %v", err)
		}
	})
	blocked, err := DryRun(t.Context(), badSource, productionExpectations())
	if err != nil {
		t.Fatalf("invalid port dry-run: %v", err)
	}
	if blocked.Ready {
		t.Fatalf("invalid stream port unexpectedly accepted")
	}
}

func TestImportMapsLegacyLocalFFmpegRecipeToH264LiveOutput(t *testing.T) {
	t.Parallel()
	source, _ := createLegacyFixture(t, func(db *sql.DB) {
		derived := "ffmpeg:rtsp://127.0.0.1:8554/소방서1#video=h264#width=1920#height=1080"
		if _, err := db.Exec(`UPDATE cameras SET sub_stream_url=? WHERE id='소방서1'`, derived); err != nil {
			t.Fatalf("set derived live fixture: %v", err)
		}
	})
	target := filepath.Join(t.TempDir(), "derived-live.db")

	manifest, err := Import(t.Context(), source, target, productionExpectations())
	if err != nil || !manifest.Ready || manifest.Summary.SubStreamCount != 9 {
		t.Fatalf("derived live import = ready:%v summary:%#v blockers:%#v err=%v", manifest.Ready, manifest.Summary, manifest.Blockers, err)
	}
	foundWarning := false
	for _, warning := range manifest.Warnings {
		if warning.Code == "CAMERA_DERIVED_LIVE_MAPPED" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("derived live mapping warning missing: %#v", manifest.Warnings)
	}

	db, err := store.Open(target)
	if err != nil {
		t.Fatalf("open imported target: %v", err)
	}
	defer db.Close()
	cameras, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatalf("list imported cameras: %v", err)
	}
	for _, camera := range cameras {
		if camera.StreamName != "소방서1" {
			continue
		}
		if len(camera.Streams) != 1 || camera.Streams[0].SourceKey != "recording" {
			t.Fatalf("derived recipe became a recursive input: %#v", camera.Streams)
		}
		live := camera.Outputs[1]
		if live.SourceKey != "recording" || live.VideoMode != store.CameraVideoH264 || live.MaxWidth == nil || live.MaxHeight == nil || *live.MaxWidth != 1920 || *live.MaxHeight != 1080 {
			t.Fatalf("derived live output = %#v", live)
		}
		return
	}
	t.Fatal("derived live camera missing")
}

func TestImportRejectsSymlinkTarget(t *testing.T) {
	t.Parallel()
	source, _ := createLegacyFixture(t, nil)
	directory := t.TempDir()
	realTarget := filepath.Join(directory, "operator-owned.db")
	if err := os.WriteFile(realTarget, []byte("operator-owned"), 0o600); err != nil {
		t.Fatalf("write real target: %v", err)
	}
	symlinkTarget := filepath.Join(directory, "target.db")
	if err := os.Symlink(realTarget, symlinkTarget); err != nil {
		t.Fatalf("create target symlink: %v", err)
	}

	manifest, err := Import(t.Context(), source, symlinkTarget, productionExpectations())
	if err != nil {
		t.Fatalf("symlink target result: %v", err)
	}
	if manifest.Ready || manifest.TargetStatus != "rejected" {
		t.Fatalf("symlink target unexpectedly accepted: %#v", manifest)
	}
	content, err := os.ReadFile(realTarget)
	if err != nil || string(content) != "operator-owned" {
		t.Fatalf("symlink target destination was modified")
	}
}
