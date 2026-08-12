package viewerrelease_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"camstation/internal/viewerrelease"
)

var testPublishedAt = time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)

func TestCatalogFullyVerifiesLegacyReleaseWithoutCachingAndPinsOpenFile(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current")
	artifact := []byte("legacy installer")
	writeReleaseFixtureAtDir(t, current, artifact, "")

	catalog := viewerrelease.NewCatalog(root)
	release, err := catalog.Current(t.Context())
	if err != nil || release.Version != "2.0.0-dev.1" {
		t.Fatalf("legacy current release = %#v err=%v", release, err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.Current(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy release was cached: error=%v, want cancelled re-verification", err)
	}

	_, file, err := catalog.Open(t.Context())
	if err != nil {
		t.Fatalf("open verified legacy release: %v", err)
	}
	defer file.Close()
	artifactPath := filepath.Join(current, release.Filename)
	replacement := filepath.Join(current, ".replacement.exe")
	if err := os.WriteFile(replacement, []byte("tampered legacy"), 0o444); err != nil {
		t.Fatalf("write legacy replacement: %v", err)
	}
	if err := os.Rename(replacement, artifactPath); err != nil {
		t.Fatalf("replace legacy artifact: %v", err)
	}
	contents, err := io.ReadAll(file)
	if err != nil || string(contents) != string(artifact) {
		t.Fatalf("pinned legacy contents = %q err=%v", contents, err)
	}
	if _, _, err := catalog.Open(t.Context()); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("replaced legacy artifact error = %v, want ErrInvalid", err)
	}
}

func TestCatalogDoesNotFallbackToLegacyWhenActivePointerIsInvalid(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "current")
	writeReleaseFixtureAtDir(t, current, []byte("legacy installer"), "")
	if err := os.Symlink(filepath.Join("..", "releases", "missing"), filepath.Join(current, "active")); err != nil {
		t.Fatalf("create invalid active pointer: %v", err)
	}

	catalog := viewerrelease.NewCatalog(root)
	if _, err := catalog.Current(t.Context()); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("invalid active pointer error = %v, want ErrInvalid without legacy fallback", err)
	}
}

func TestCatalogHashesOncePerImmutableArtifactIdentity(t *testing.T) {
	root := t.TempDir()
	artifactPath := writeImmutableReleaseFixture(t, root, "v1", "2.0.0", []byte("installer v1"))
	catalog := viewerrelease.NewCatalog(root)

	if _, err := catalog.Current(t.Context()); err != nil {
		t.Fatalf("verify first identity: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.Current(cancelled); err != nil {
		t.Fatalf("cached identity performed another hash: %v", err)
	}

	replacement := filepath.Join(filepath.Dir(artifactPath), ".replacement.exe")
	if err := os.WriteFile(replacement, []byte("installer v1"), 0o444); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, artifactPath); err != nil {
		t.Fatalf("replace artifact identity: %v", err)
	}
	if _, err := catalog.Current(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("changed identity error = %v, want cancelled re-verification", err)
	}
	if _, err := catalog.Current(t.Context()); err != nil {
		t.Fatalf("verify replacement identity: %v", err)
	}
	if _, err := catalog.Current(cancelled); err != nil {
		t.Fatalf("replacement identity was hashed more than once: %v", err)
	}
}

func TestCatalogInvalidatesPointerManifestAndArtifactChanges(t *testing.T) {
	root := t.TempDir()
	writeImmutableReleaseFixture(t, root, "v1", "2.0.0", []byte("installer v1"))
	catalog := viewerrelease.NewCatalog(root)
	first, err := catalog.Current(t.Context())
	if err != nil || first.Version != "2.0.0" {
		t.Fatalf("first release = %#v err=%v", first, err)
	}

	writeImmutableReleaseFixture(t, root, "v2", "2.1.0", []byte("installer v2"))
	second, err := catalog.Current(t.Context())
	if err != nil || second.Version != "2.1.0" || second.SHA256 == first.SHA256 {
		t.Fatalf("pointer-switched release = %#v err=%v", second, err)
	}

	manifestPath := filepath.Join(root, "releases", "v2", "release.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(manifest, []byte("\n{}")...), 0o444); err != nil {
		t.Fatalf("mutate manifest: %v", err)
	}
	if _, err := catalog.Current(t.Context()); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("mutated manifest error = %v, want ErrInvalid", err)
	}

	writeImmutableReleaseFixture(t, root, "v3", "2.2.0", []byte("installer v3"))
	if _, err := catalog.Current(t.Context()); err != nil {
		t.Fatalf("verify v3: %v", err)
	}
	artifactPath := filepath.Join(root, "releases", "v3", "CamStationViewerSetup.exe")
	replacement := filepath.Join(filepath.Dir(artifactPath), ".tampered.exe")
	if err := os.WriteFile(replacement, []byte("tampered v3!"), 0o444); err != nil {
		t.Fatalf("write tampered artifact: %v", err)
	}
	if err := os.Rename(replacement, artifactPath); err != nil {
		t.Fatalf("replace artifact: %v", err)
	}
	if _, _, err := catalog.Open(t.Context()); !errors.Is(err, viewerrelease.ErrInvalid) {
		t.Fatalf("mutated artifact download error = %v, want ErrInvalid", err)
	}
}

func TestCatalogOpenReturnsPinnedVerifiedArtifactWithoutSecondHash(t *testing.T) {
	root := t.TempDir()
	writeImmutableReleaseFixture(t, root, "v1", "2.0.0", []byte("installer v1"))
	catalog := viewerrelease.NewCatalog(root)
	if _, err := catalog.Current(t.Context()); err != nil {
		t.Fatalf("verify v1: %v", err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	release, file, err := catalog.Open(cancelled)
	if err != nil {
		t.Fatalf("cached download performed another hash: %v", err)
	}
	defer file.Close()
	if release.Version != "2.0.0" {
		t.Fatalf("opened release = %#v", release)
	}
	writeImmutableReleaseFixture(t, root, "v2", "2.1.0", []byte("installer v2"))
	contents, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read pinned artifact: %v", err)
	}
	if string(contents) != "installer v1" {
		t.Fatalf("pinned download contents = %q, want v1", contents)
	}
}

func writeImmutableReleaseFixture(t *testing.T, root, id, version string, artifact []byte) string {
	t.Helper()
	dir := filepath.Join(root, "releases", id)
	digest := sha256.Sum256(artifact)
	manifest := releaseManifest{
		Version:             version,
		Filename:            "CamStationViewerSetup.exe",
		SizeBytes:           int64(len(artifact)),
		SHA256:              hex.EncodeToString(digest[:]),
		PublishedAt:         testPublishedAt,
		DevelopmentUnsigned: true,
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create immutable release: %v", err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal immutable manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), encoded, 0o444); err != nil {
		t.Fatalf("write immutable manifest: %v", err)
	}
	artifactPath := filepath.Join(dir, manifest.Filename)
	if err := os.WriteFile(artifactPath, artifact, 0o444); err != nil {
		t.Fatalf("write immutable artifact: %v", err)
	}
	current := filepath.Join(root, "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatalf("create current directory: %v", err)
	}
	temporary := filepath.Join(current, ".active.new")
	_ = os.Remove(temporary)
	if err := os.Symlink(filepath.Join("..", "releases", id), temporary); err != nil {
		t.Fatalf("create active pointer: %v", err)
	}
	if err := os.Rename(temporary, filepath.Join(current, "active")); err != nil {
		t.Fatalf("switch active pointer: %v", err)
	}
	return artifactPath
}
