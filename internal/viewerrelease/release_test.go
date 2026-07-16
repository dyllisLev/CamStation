package viewerrelease_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camstation/internal/viewerrelease"
)

func TestLoadRejectsMissingAndMismatchedArtifact(t *testing.T) {
	dir := t.TempDir()
	if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrUnavailable) {
		t.Fatalf("missing release error = %v", err)
	}

	writeReleaseFixture(t, dir, []byte("installer"), "00")
	if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("bad digest error = %v", err)
	}
}

func TestLoadRejectsUnsafeManifestFields(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		size     int64
		digest   string
	}{
		{name: "absolute filename", filename: filepath.Join(string(filepath.Separator), "CamStationViewerSetup.exe"), size: 9, digest: strings.Repeat("0", 64)},
		{name: "slash", filename: "nested/setup.exe", size: 9, digest: strings.Repeat("0", 64)},
		{name: "backslash", filename: `nested\setup.exe`, size: 9, digest: strings.Repeat("0", 64)},
		{name: "wrong extension", filename: "CamStationViewerSetup.zip", size: 9, digest: strings.Repeat("0", 64)},
		{name: "zero size", filename: "CamStationViewerSetup.exe", size: 0, digest: strings.Repeat("0", 64)},
		{name: "short digest", filename: "CamStationViewerSetup.exe", size: 9, digest: "00"},
		{name: "uppercase digest", filename: "CamStationViewerSetup.exe", size: 9, digest: strings.Repeat("A", 64)},
		{name: "non hex digest", filename: "CamStationViewerSetup.exe", size: 9, digest: strings.Repeat("z", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, releaseManifest{
				Version:     "2.0.0-dev.1",
				Filename:    tt.filename,
				SizeBytes:   tt.size,
				SHA256:      tt.digest,
				PublishedAt: time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC),
			})
			if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrInvalid) {
				t.Fatalf("Load() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestLoadRejectsUnknownAndTrailingManifestData(t *testing.T) {
	dir := t.TempDir()
	artifact := []byte("installer")
	digest := sha256.Sum256(artifact)
	manifest := `{"version":"2.0.0-dev.1","filename":"CamStationViewerSetup.exe","sizeBytes":9,"sha256":"` + hex.EncodeToString(digest[:]) + `","publishedAt":"2026-07-16T01:02:03Z","developmentUnsigned":true,"unexpected":true}`
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CamStationViewerSetup.exe"), artifact, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("unknown field error = %v, want ErrInvalid", err)
	}

	writeReleaseFixture(t, dir, artifact, "")
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "release.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), append(manifestBytes, []byte("\n{}")...), 0o644); err != nil {
		t.Fatalf("append manifest data: %v", err)
	}
	if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("trailing data error = %v, want ErrInvalid", err)
	}
}

func TestLoadRejectsMissingAndWrongSizedArtifactWithoutLeakingDirectory(t *testing.T) {
	dir := t.TempDir()
	artifact := []byte("installer")
	digest := sha256.Sum256(artifact)
	writeManifest(t, dir, releaseManifest{
		Version:     "2.0.0-dev.1",
		Filename:    "CamStationViewerSetup.exe",
		SizeBytes:   int64(len(artifact)),
		SHA256:      hex.EncodeToString(digest[:]),
		PublishedAt: time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC),
	})
	if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrUnavailable) || strings.Contains(err.Error(), dir) {
		t.Fatalf("missing artifact error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "CamStationViewerSetup.exe"), []byte("short"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrInvalid) || strings.Contains(err.Error(), dir) {
		t.Fatalf("wrong size error = %v", err)
	}
}

func TestLoadAndOpenVerified(t *testing.T) {
	dir := t.TempDir()
	artifact := []byte("installer")
	writeReleaseFixture(t, dir, artifact, "")

	release, err := viewerrelease.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if release.Version != "2.0.0-dev.1" || release.Filename != "CamStationViewerSetup.exe" || release.SizeBytes != int64(len(artifact)) {
		t.Fatalf("release = %#v", release)
	}
	if release.DownloadURL() != "/api/viewers/app/download" {
		t.Fatalf("DownloadURL() = %q", release.DownloadURL())
	}

	file, err := release.OpenVerified()
	if err != nil {
		t.Fatalf("OpenVerified() error = %v", err)
	}
	defer file.Close()
	contents, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read verified file: %v", err)
	}
	if string(contents) != string(artifact) {
		t.Fatalf("verified contents = %q", contents)
	}
}

func TestOpenVerifiedRejectsArtifactChangedAfterLoad(t *testing.T) {
	dir := t.TempDir()
	writeReleaseFixture(t, dir, []byte("installer"), "")
	release, err := viewerrelease.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, release.Filename), []byte("tampered!"), 0o644); err != nil {
		t.Fatalf("tamper artifact: %v", err)
	}
	if _, err := release.OpenVerified(); !errors.Is(err, viewerrelease.ErrInvalid) || strings.Contains(err.Error(), dir) {
		t.Fatalf("OpenVerified() error = %v, want ErrInvalid without directory", err)
	}
}

func TestLoadRejectsArtifactSymlinkOutsideReleaseDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "current")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create release dir: %v", err)
	}
	artifact := []byte("outside installer")
	outsidePath := filepath.Join(root, "outside.exe")
	if err := os.WriteFile(outsidePath, artifact, 0o644); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(dir, "CamStationViewerSetup.exe")); err != nil {
		t.Skipf("create artifact symlink: %v", err)
	}
	digest := sha256.Sum256(artifact)
	writeManifest(t, dir, releaseManifest{
		Version:     "2.0.0-dev.1",
		Filename:    "CamStationViewerSetup.exe",
		SizeBytes:   int64(len(artifact)),
		SHA256:      hex.EncodeToString(digest[:]),
		PublishedAt: time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC),
	})

	if _, err := viewerrelease.Load(dir); err == nil || strings.Contains(err.Error(), root) {
		t.Fatalf("escaping symlink error = %v", err)
	}
}

type releaseManifest struct {
	Version             string    `json:"version"`
	Filename            string    `json:"filename"`
	SizeBytes           int64     `json:"sizeBytes"`
	SHA256              string    `json:"sha256"`
	PublishedAt         time.Time `json:"publishedAt"`
	DevelopmentUnsigned bool      `json:"developmentUnsigned"`
}

func writeReleaseFixture(t *testing.T, dir string, artifact []byte, digestOverride string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create release dir: %v", err)
	}
	digest := sha256.Sum256(artifact)
	digestHex := hex.EncodeToString(digest[:])
	if digestOverride != "" {
		digestHex = digestOverride
	}
	writeManifest(t, dir, releaseManifest{
		Version:             "2.0.0-dev.1",
		Filename:            "CamStationViewerSetup.exe",
		SizeBytes:           int64(len(artifact)),
		SHA256:              digestHex,
		PublishedAt:         time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC),
		DevelopmentUnsigned: true,
	})
	if err := os.WriteFile(filepath.Join(dir, "CamStationViewerSetup.exe"), artifact, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}

func writeManifest(t *testing.T, dir string, manifest releaseManifest) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create release dir: %v", err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), encoded, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}
