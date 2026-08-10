package legacyimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/store"
)

func writeGo2RTCFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write go2rtc fixture: %v", err)
	}
	return path
}

func go2RTCCanaryFixture() (string, []string) {
	secrets := []string{"home-user", "home-secret", "home-token"}
	return `api:
  listen: 127.0.0.1:1984
streams:
  집-마당: "rtsp://home-user:home-secret@192.0.2.10/main?token=home-token"
  집-마당_sub: "rtsp://home-user:home-secret@192.0.2.10/sub?token=home-token"
  집-창고1: "rtsp://home-user:home-secret@192.0.2.11/main?token=home-token"
  집-창고1_sub: "rtsp://home-user:home-secret@192.0.2.11/sub?token=home-token"
  집-창고2: "rtsp://home-user:home-secret@192.0.2.12/main?token=home-token"
  집-창고2_sub: "rtsp://home-user:home-secret@192.0.2.12/sub?token=home-token"
  소방서1: "rtsp://fire-user:fire-secret@192.0.2.20/main"
  소방서1_sub: "rtsp://fire-user:fire-secret@192.0.2.20/sub"
#  소방서2: "rtsp://disabled-user:disabled-secret@192.0.2.21/main"
#  소방서2_sub: "rtsp://disabled-user:disabled-secret@192.0.2.21/sub"
`, secrets
}

func TestImportGo2RTCCanaryCreatesOnlySelectedActiveStreams(t *testing.T) {
	t.Parallel()
	content, secrets := go2RTCCanaryFixture()
	source := writeGo2RTCFixture(t, content)
	target := filepath.Join(t.TempDir(), "canary.db")

	manifest, err := ImportGo2RTCCanary(t.Context(), source, target, Go2RTCCanaryOptions{
		Prefix:          "집-",
		ExpectedCameras: 3,
	})
	if err != nil {
		t.Fatalf("import go2rtc canary: %v", err)
	}
	if !manifest.Ready || manifest.Operation != "go2rtc-canary" || manifest.TargetStatus != "created" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.SourceKind != "go2rtc-yaml" || manifest.SourceFingerprint == "" || manifest.CanonicalFingerprint == "" {
		t.Fatalf("manifest provenance = %#v", manifest)
	}
	sourceSum := sha256.Sum256([]byte(content))
	if manifest.SourceFingerprint != hex.EncodeToString(sourceSum[:]) {
		t.Fatalf("source fingerprint = %q", manifest.SourceFingerprint)
	}
	if manifest.Summary.CameraCount != 3 || manifest.Summary.EnabledCount != 3 || manifest.Summary.DisabledCount != 0 || manifest.Summary.SubStreamCount != 3 {
		t.Fatalf("summary = %#v", manifest.Summary)
	}
	if manifest.Summary.LayoutCount != 1 || manifest.Summary.LayoutItemCount != 3 {
		t.Fatalf("layout summary = %#v", manifest.Summary)
	}
	if manifest.Settings.SegmentMinutes != 1 || manifest.Settings.RetentionDays != 1 || manifest.Settings.MaxStorageGB != 20 || manifest.Settings.BackupEnabled {
		t.Fatalf("settings = %#v", manifest.Settings)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range append(secrets, "fire-secret", "disabled-secret", "rtsp://") {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("manifest leaked producer data")
		}
	}

	db, err := store.Open(target)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer db.Close()
	cameras, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatalf("list cameras: %v", err)
	}
	wantKeys := []string{"집-마당", "집-창고1", "집-창고2"}
	if len(cameras) != len(wantKeys) {
		t.Fatalf("camera count = %d", len(cameras))
	}
	for index, camera := range cameras {
		if camera.StreamName != wantKeys[index] || camera.Name != wantKeys[index] || !camera.Enabled {
			t.Fatalf("camera[%d] = key:%q name:%q enabled:%v", index, camera.StreamName, camera.Name, camera.Enabled)
		}
		if len(camera.Streams) != 2 || camera.Streams[0].SourceKey != "recording" || camera.Streams[1].SourceKey != "live" {
			t.Fatalf("camera graph = %#v", camera.Streams)
		}
		if len(camera.Outputs) != 3 || camera.PolicyState.DesiredRevision != 1 || camera.PolicyState.AppliedRevision != 0 {
			t.Fatalf("camera policy = outputs:%d state:%#v", len(camera.Outputs), camera.PolicyState)
		}
		for _, stream := range camera.Streams {
			if !strings.Contains(stream.URL, "home-secret") || strings.Contains(stream.URL, "fire-secret") {
				t.Fatalf("selected private input is incorrect")
			}
		}
	}
	settings, err := db.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings.Recording.SegmentMinutes != 1 || settings.Recording.RetentionDays != 1 || settings.Recording.MaxStorageGB != 20 ||
		settings.Backup.Enabled || settings.Backup.ScheduleEnabled || settings.Alerts.DiscordEnabled {
		t.Fatalf("unsafe canary settings: %#v", settings)
	}
	layouts, err := db.ListLayouts(t.Context())
	if err != nil || len(layouts) != 1 || len(layouts[0].Data) != 3 {
		t.Fatalf("layouts = %#v err=%v", layouts, err)
	}
}

func TestImportGo2RTCCanaryFailsClosedWithoutPromotingTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name: "missing sub",
			content: `streams:
  집-마당: "rtsp://user:secret@192.0.2.10/main"
`,
			expected: 1,
		},
		{
			name: "loopback source",
			content: `streams:
  집-마당: "rtsp://127.0.0.1:8554/집-마당"
  집-마당_sub: "rtsp://127.0.0.1:8554/집-마당_sub"
`,
			expected: 1,
		},
		{
			name: "ffmpeg recipe",
			content: `streams:
  집-마당: "ffmpeg:rtsp://192.0.2.10/main#video=copy"
  집-마당_sub: "rtsp://192.0.2.10/sub"
`,
			expected: 1,
		},
		{
			name: "multiple producers",
			content: `streams:
  집-마당:
    - "rtsp://192.0.2.10/main"
    - "rtsp://192.0.2.11/main"
  집-마당_sub: "rtsp://192.0.2.10/sub"
`,
			expected: 1,
		},
		{
			name: "unexpected selected count",
			content: `streams:
  집-마당: "rtsp://192.0.2.10/main"
  집-마당_sub: "rtsp://192.0.2.10/sub"
`,
			expected: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := writeGo2RTCFixture(t, tc.content)
			target := filepath.Join(t.TempDir(), "must-not-exist.db")
			manifest, err := ImportGo2RTCCanary(t.Context(), source, target, Go2RTCCanaryOptions{
				Prefix: "집-", ExpectedCameras: tc.expected,
			})
			if err != nil {
				t.Fatalf("infrastructure error: %v", err)
			}
			if manifest.Ready || len(manifest.Blockers) == 0 {
				t.Fatalf("unsafe source accepted: %#v", manifest)
			}
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Fatalf("blocked import promoted target: %v", statErr)
			}
		})
	}
}

func TestImportGo2RTCCanaryNeverOverwritesExistingTarget(t *testing.T) {
	t.Parallel()
	content, _ := go2RTCCanaryFixture()
	source := writeGo2RTCFixture(t, content)
	target := filepath.Join(t.TempDir(), "existing.db")
	if err := os.WriteFile(target, []byte("operator-owned"), 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := ImportGo2RTCCanary(t.Context(), source, target, Go2RTCCanaryOptions{Prefix: "집-", ExpectedCameras: 3})
	if err != nil {
		t.Fatalf("existing target result: %v", err)
	}
	if manifest.Ready || manifest.TargetStatus != "rejected" {
		t.Fatalf("existing target accepted: %#v", manifest)
	}
	contentAfter, err := os.ReadFile(target)
	if err != nil || string(contentAfter) != "operator-owned" {
		t.Fatalf("existing target changed")
	}
}
