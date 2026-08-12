package store

import "testing"

func TestCameraStorePreservesSafeHangulStreamKey(t *testing.T) {
	t.Parallel()

	db := openMigratedStore(t)
	camera := mustCamera(t, db, "집-마당")
	if camera.StreamName != "집-마당" || camera.LayoutKey != "집-마당" {
		t.Fatalf("camera keys = %q/%q, want preserved Hangul key", camera.StreamName, camera.LayoutKey)
	}

	camera.Name = "집 마당 카메라"
	updated, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision))
	if err != nil {
		t.Fatalf("update Hangul-key camera: %v", err)
	}
	if updated.StreamName != "집-마당" || updated.LayoutKey != "집-마당" {
		t.Fatalf("updated keys = %q/%q, want stable Hangul key", updated.StreamName, updated.LayoutKey)
	}
}

func TestCameraStoreRejectsUnsafeStreamKey(t *testing.T) {
	t.Parallel()

	db := openMigratedStore(t)
	if _, err := db.UpsertCamera(t.Context(), Camera{
		Name: "unsafe", URL: "rtsp://camera.invalid/main", StreamName: "../unsafe", State: "unknown",
	}); err == nil {
		t.Fatal("UpsertCamera accepted unsafe stream key")
	}
}
