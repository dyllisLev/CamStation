package store

import (
	"testing"
	"time"
)

func TestCameraPresetNamesPersistReconcileAndCascade(t *testing.T) {
	db := openMigratedStore(t)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("idempotent migrate: %v", err)
	}
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "Preset Camera", StreamName: "preset-camera", URL: "rtsp://camera.invalid/live", State: "streaming",
	})
	if err != nil {
		t.Fatalf("upsert camera: %v", err)
	}
	for token, name := range map[string]string{"PRESET_0": "입구", "PRESET_1": "창고", "PRESET_2": "후문"} {
		if err := db.UpsertCameraPresetName(t.Context(), camera.ID, token, name); err != nil {
			t.Fatalf("upsert %s: %v", token, err)
		}
	}
	if err := db.UpsertCameraPresetName(t.Context(), camera.ID, "PRESET_0", "정문"); err != nil {
		t.Fatalf("update name: %v", err)
	}
	names, err := db.ListCameraPresetNames(t.Context(), camera.ID)
	if err != nil || names["PRESET_0"] != "정문" || names["PRESET_1"] != "창고" {
		t.Fatalf("names/error = %#v/%v", names, err)
	}
	cutoff := time.Now().UTC()
	if err := db.UpsertCameraPresetName(t.Context(), camera.ID, "PRESET_1", "새 창고"); err != nil {
		t.Fatalf("update concurrent name: %v", err)
	}
	if err := db.ReconcileCameraPresetNames(t.Context(), camera.ID, []string{"PRESET_0"}, cutoff); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	names, err = db.ListCameraPresetNames(t.Context(), camera.ID)
	if err != nil || len(names) != 2 || names["PRESET_0"] != "정문" || names["PRESET_1"] != "새 창고" {
		t.Fatalf("reconciled names/error = %#v/%v", names, err)
	}
	if err := db.DeleteCameraPresetName(t.Context(), camera.ID, "PRESET_0"); err != nil {
		t.Fatalf("delete name: %v", err)
	}
	if err := db.UpsertCameraPresetName(t.Context(), camera.ID, "PRESET_2", "후문"); err != nil {
		t.Fatalf("upsert cascade row: %v", err)
	}
	if _, err := db.DeleteCamera(t.Context(), camera.StreamName); err != nil {
		t.Fatalf("delete camera: %v", err)
	}
	names, err = db.ListCameraPresetNames(t.Context(), camera.ID)
	if err != nil || len(names) != 0 {
		t.Fatalf("cascade names/error = %#v/%v", names, err)
	}
}
