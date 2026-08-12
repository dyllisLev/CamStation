package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func openMigratedStore(t *testing.T) *DB {
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

func createTemplateBackedCamera(t *testing.T, db *DB) (CameraProfileTemplate, Camera) {
	t.Helper()

	template, err := db.CreateCameraProfileTemplate(t.Context(), testProfileTemplate())
	if err != nil {
		t.Fatalf("create profile template: %v", err)
	}
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name:              "염소장",
		URL:               "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
		StreamName:        "goat-yard",
		State:             "streaming",
		Manufacturer:      "VStarcam",
		Model:             "V400D",
		ProfileAdapter:    "vstarcam",
		Host:              "192.168.0.55",
		RTSPPort:          10554,
		HTTPPort:          10080,
		ONVIFPort:         10080,
		ProfileTemplateID: &template.ID,
	})
	if err != nil {
		t.Fatalf("upsert camera: %v", err)
	}
	return template, camera
}

func testProfileTemplate() CameraProfileTemplate {
	return CameraProfileTemplate{
		ProfileName:  "Dual Lens",
		Manufacturer: "VStarcam",
		Model:        "V400D",
		Adapter:      "vstarcam",
		Version:      1,
		MatchRules: []CameraProfileMatchRule{
			{Field: "manufacturer", Operator: "equals", Value: "VStarcam"},
			{Field: "model", Operator: "contains", Value: "V400D"},
		},
		Channels: []CameraProfileTemplateChannel{
			{
				Index: 0,
				Name:  "lens-1",
				Streams: []CameraProfileTemplateStream{
					{
						Role:         CameraStreamRoleRecording,
						Label:        "main",
						Source:       "onvif",
						Path:         "/tcp/av0_0",
						ProfileToken: "PROFILE_000",
						Codec:        "h264",
						Width:        2304,
						Height:       1296,
						FPS:          12,
						BitrateKbps:  1024,
					},
					{
						Role:         CameraStreamRoleLive,
						Label:        "sub",
						Source:       "onvif",
						Path:         "/tcp/av0_1",
						ProfileToken: "PROFILE_001",
						Codec:        "h264",
						Width:        448,
						Height:       256,
						FPS:          12,
						BitrateKbps:  512,
					},
				},
			},
		},
		Capabilities: CameraProfileCapabilities{
			ONVIF:        true,
			RTSP:         true,
			MultiChannel: true,
		},
	}
}

func assertTemplatePublicJSONHasNoCredentials(t *testing.T, template CameraProfileTemplate) {
	t.Helper()

	encoded, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("marshal profile template: %v", err)
	}
	publicJSON := string(encoded)
	for _, forbidden := range []string{"admin", "secret", "password", "username", "rtsp://", "@"} {
		if strings.Contains(publicJSON, forbidden) {
			t.Fatalf("profile template JSON leaked credential marker %q: %s", forbidden, publicJSON)
		}
	}
}
