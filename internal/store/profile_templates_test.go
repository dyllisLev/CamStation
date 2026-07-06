package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileTemplateCreateListGetUpdateDelete_whenTemplateIsCredentialFree(t *testing.T) {
	t.Parallel()

	// Given
	db := openMigratedStore(t)
	template := testProfileTemplate()

	// When
	created, err := db.CreateCameraProfileTemplate(t.Context(), template)

	// Then
	if err != nil {
		t.Fatalf("create profile template: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("created template id = 0")
	}
	assertTemplatePublicJSONHasNoCredentials(t, created)

	listed, err := db.ListCameraProfileTemplates(t.Context())
	if err != nil {
		t.Fatalf("list profile templates: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed templates = %#v, want created id %d", listed, created.ID)
	}

	got, err := db.GetCameraProfileTemplate(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("get profile template: %v", err)
	}
	if got.Channels[0].Streams[0].Path != "/tcp/av0_0" {
		t.Fatalf("recording path = %q, want /tcp/av0_0", got.Channels[0].Streams[0].Path)
	}

	got.ProfileName = "Dual Lens Updated"
	got.Capabilities.Snapshot = true
	updated, err := db.UpdateCameraProfileTemplate(t.Context(), got.ID, got)
	if err != nil {
		t.Fatalf("update profile template: %v", err)
	}
	if updated.ProfileName != "Dual Lens Updated" || !updated.Capabilities.Snapshot {
		t.Fatalf("updated template = %#v", updated)
	}

	if err := db.DeleteCameraProfileTemplate(t.Context(), updated.ID); err != nil {
		t.Fatalf("delete unreferenced profile template: %v", err)
	}
	if _, err := db.GetCameraProfileTemplate(t.Context(), updated.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted template err = %v, want ErrNotFound", err)
	}
}

func TestProfileTemplateCreate_rejectsDuplicateNormalizedKeyForSameVersion(t *testing.T) {
	t.Parallel()

	// Given
	db := openMigratedStore(t)
	template := testProfileTemplate()
	created, err := db.CreateCameraProfileTemplate(t.Context(), template)
	if err != nil {
		t.Fatalf("create profile template: %v", err)
	}

	duplicate := testProfileTemplate()
	duplicate.Adapter = " VSTARCAM "
	duplicate.Manufacturer = " vstarcam "
	duplicate.Model = " V400D  "
	duplicate.ProfileName = " dual   lens "
	duplicate.Version = created.Version

	// When
	_, duplicateErr := db.CreateCameraProfileTemplate(t.Context(), duplicate)

	// Then
	if !errors.Is(duplicateErr, ErrProfileTemplateDuplicate) {
		t.Fatalf("duplicate err = %v, want ErrProfileTemplateDuplicate", duplicateErr)
	}

	duplicate.Version = created.Version + 1
	versioned, err := db.CreateCameraProfileTemplate(t.Context(), duplicate)
	if err != nil {
		t.Fatalf("create versioned profile template: %v", err)
	}
	if versioned.Version != 2 {
		t.Fatalf("versioned template version = %d, want 2", versioned.Version)
	}
}

func TestCameraProfileTemplateProvenanceAndStreamsSnapshot_whenTemplateChangesLater(t *testing.T) {
	t.Parallel()

	// Given
	db := openMigratedStore(t)
	template, camera := createTemplateBackedCamera(t, db)
	streams := []CameraStream{
		{
			Role:             CameraStreamRoleRecording,
			Label:            "lens 1 main",
			Source:           "profile-template",
			URL:              "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
			Go2RTCStreamName: "goat-yard-recording",
			ProfileToken:     "PROFILE_000",
			State:            "streaming",
		},
		{
			Role:             CameraStreamRoleLive,
			Label:            "lens 1 sub",
			Source:           "profile-template",
			URL:              "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_1",
			Go2RTCStreamName: "goat-yard-live",
			ProfileToken:     "PROFILE_001",
			State:            "streaming",
		},
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, streams); err != nil {
		t.Fatalf("replace camera streams: %v", err)
	}

	// When
	template.Channels[0].Streams[0].Path = "/changed-after-camera-save"
	if _, err := db.UpdateCameraProfileTemplate(t.Context(), template.ID, template); err != nil {
		t.Fatalf("update profile template: %v", err)
	}

	// Then
	cameras, err := db.ListCameras(t.Context(), false)
	if err != nil {
		t.Fatalf("list cameras: %v", err)
	}
	if len(cameras) != 1 {
		t.Fatalf("cameras = %d, want 1", len(cameras))
	}
	if cameras[0].ProfileTemplateID == nil || *cameras[0].ProfileTemplateID != template.ID {
		t.Fatalf("profile template provenance = %v, want %d", cameras[0].ProfileTemplateID, template.ID)
	}
	if cameras[0].Streams[0].ProfileToken != "PROFILE_000" || cameras[0].Streams[0].Go2RTCStreamName != "goat-yard-recording" {
		t.Fatalf("recording stream snapshot = %#v", cameras[0].Streams[0])
	}
	if strings.Contains(cameras[0].Streams[0].RedactedURL, "secret") || cameras[0].Streams[0].URL != "" {
		t.Fatalf("public camera stream leaked credentials: %#v", cameras[0].Streams[0])
	}
}

func TestProfileTemplateDelete_blocksReferencedTemplateUntilCameraDetached(t *testing.T) {
	t.Parallel()

	// Given
	db := openMigratedStore(t)
	template, camera := createTemplateBackedCamera(t, db)

	// When
	deleteReferencedErr := db.DeleteCameraProfileTemplate(t.Context(), template.ID)

	// Then
	if !errors.Is(deleteReferencedErr, ErrProfileTemplateInUse) {
		t.Fatalf("delete referenced err = %v, want ErrProfileTemplateInUse", deleteReferencedErr)
	}

	camera.ProfileTemplateID = nil
	if _, err := db.UpsertCamera(t.Context(), camera); err != nil {
		t.Fatalf("detach camera template: %v", err)
	}
	if err := db.DeleteCameraProfileTemplate(t.Context(), template.ID); err != nil {
		t.Fatalf("delete detached profile template: %v", err)
	}
}

func TestMigrate_preservesLegacyCameraRowsWithoutProfileTemplateID(t *testing.T) {
	t.Parallel()

	// Given
	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if _, err := db.db.ExecContext(t.Context(), `CREATE TABLE cameras (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		stream_name TEXT NOT NULL UNIQUE,
		layout_key TEXT NOT NULL DEFAULT '',
		recording_stream_name TEXT NOT NULL DEFAULT '',
		live_stream_name TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL,
		manufacturer TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		profile_adapter TEXT NOT NULL DEFAULT '',
		host TEXT NOT NULL DEFAULT '',
		rtsp_port INTEGER NOT NULL DEFAULT 0,
		http_port INTEGER NOT NULL DEFAULT 0,
		onvif_port INTEGER NOT NULL DEFAULT 0,
		channel_index INTEGER,
		last_probe_json TEXT NOT NULL DEFAULT '{}',
		last_scan_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy cameras table: %v", err)
	}
	if _, err := db.db.ExecContext(t.Context(), `INSERT INTO cameras(
		name, url, stream_name, layout_key, state, created_at, updated_at
	) VALUES ('legacy', 'rtsp://admin:secret@192.168.0.5/stream1', 'legacy-camera', 'legacy-camera', 'streaming', '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy camera: %v", err)
	}

	// When
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate legacy store: %v", err)
	}

	// Then
	cameras, err := db.ListCameras(t.Context(), false)
	if err != nil {
		t.Fatalf("list migrated cameras: %v", err)
	}
	if len(cameras) != 1 {
		t.Fatalf("migrated cameras = %d, want 1", len(cameras))
	}
	if cameras[0].ProfileTemplateID != nil {
		t.Fatalf("legacy camera profile template id = %v, want nil", cameras[0].ProfileTemplateID)
	}
}
