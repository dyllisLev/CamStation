package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestCameraControlCapabilitiesDefaultToUnknown(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "No controls", URL: "rtsp://camera.invalid/live",
		StreamName: "no-controls", State: "streaming",
	})
	if err != nil {
		t.Fatalf("UpsertCamera: %v", err)
	}
	if camera.ControlCapabilities.PTZ.Support != ControlSupportUnknown || camera.ControlCapabilities.PTZ.Available {
		t.Fatalf("unexpected PTZ default: %#v", camera.ControlCapabilities.PTZ)
	}
}

func TestUpdateCameraControlCapabilitiesUsesStableStreamOnly(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "염소장", URL: "rtsp://operator:secret@camera.invalid/main",
		StreamName: "goat-yard", LiveStreamName: "goat-yard-live", State: "streaming",
	})
	if err != nil {
		t.Fatalf("UpsertCamera: %v", err)
	}
	caps := CameraControlCapabilities{
		PTZ:        CameraControlFeature{Support: ControlSupportSupported, Available: true},
		Home:       CameraControlFeature{Support: ControlSupportSupported, Available: true},
		MaxPresets: 100,
	}
	if err := db.UpdateCameraControlCapabilities(t.Context(), camera.StreamName, caps); err != nil {
		t.Fatalf("UpdateCameraControlCapabilities: %v", err)
	}
	stored, err := db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil || !stored.ControlCapabilities.PTZ.Available || stored.ControlCapabilities.MaxPresets != 100 {
		t.Fatalf("stored capabilities/error = %#v/%v", stored.ControlCapabilities, err)
	}
	if err := db.UpdateCameraControlCapabilities(t.Context(), camera.LiveStreamName, caps); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("role alias error = %v, want sql.ErrNoRows", err)
	}
}

func TestCameraControlCapabilitiesMalformedJSONDefaultsToUnknown(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{StreamName: "malformed", URL: "rtsp://camera.invalid/live"})
	if err != nil {
		t.Fatalf("UpsertCamera: %v", err)
	}
	if _, err := db.db.ExecContext(t.Context(), `UPDATE cameras SET control_capabilities_json = ? WHERE id = ?`, `{broken`, camera.ID); err != nil {
		t.Fatalf("corrupt capability fixture: %v", err)
	}
	stored, err := db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil {
		t.Fatalf("GetCameraByStream: %v", err)
	}
	if stored.ControlCapabilities.PTZ.Support != ControlSupportUnknown || stored.ControlCapabilities.PTZ.Available {
		t.Fatalf("malformed default = %#v", stored.ControlCapabilities.PTZ)
	}
}
