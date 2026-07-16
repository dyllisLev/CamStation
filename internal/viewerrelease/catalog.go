package viewerrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type Catalog struct {
	rootDir string
	mu      sync.Mutex
	current *catalogEntry
}

type catalogEntry struct {
	release  Release
	target   string
	pointer  os.FileInfo
	manifest os.FileInfo
	artifact os.FileInfo
}

type selectedRelease struct {
	target     string
	releaseDir string
	pointer    os.FileInfo
}

func NewCatalog(rootDir string) *Catalog {
	return &Catalog{rootDir: rootDir}
}

func (c *Catalog) Current(ctx context.Context) (Release, error) {
	release, file, err := c.load(ctx, false)
	if file != nil {
		_ = file.Close()
	}
	return release, err
}

func (c *Catalog) Open(ctx context.Context) (Release, *os.File, error) {
	return c.load(ctx, true)
}

func (c *Catalog) load(ctx context.Context, keepArtifact bool) (Release, *os.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	root, err := os.OpenRoot(c.rootDir)
	if err != nil {
		c.current = nil
		return Release{}, nil, ErrUnavailable
	}
	defer root.Close()
	selected, err := selectImmutableRelease(root)
	if err != nil {
		c.current = nil
		return Release{}, nil, err
	}
	releaseRoot, err := root.OpenRoot(selected.releaseDir)
	if err != nil {
		c.current = nil
		return Release{}, nil, currentRootError(err)
	}
	defer releaseRoot.Close()

	if entry := c.current; entry != nil && entry.target == selected.target && sameStat(entry.pointer, selected.pointer) {
		manifest, manifestErr := regularFileInfo(releaseRoot, "release.json")
		artifact, artifactErr := regularFileInfo(releaseRoot, entry.release.Filename)
		if manifestErr == nil && artifactErr == nil && sameStat(entry.manifest, manifest) && sameStat(entry.artifact, artifact) {
			if !keepArtifact {
				return entry.release, nil, nil
			}
			file, err := releaseRoot.Open(entry.release.Filename)
			if err == nil {
				opened, statErr := file.Stat()
				if statErr == nil && sameStat(entry.artifact, opened) {
					return entry.release, file, nil
				}
				_ = file.Close()
			}
		}
	}

	c.current = nil
	entry, file, err := verifyRelease(ctx, c.rootDir, selected, releaseRoot)
	if err != nil {
		return Release{}, nil, err
	}
	c.current = &entry
	if keepArtifact {
		return entry.release, file, nil
	}
	_ = file.Close()
	return entry.release, nil, nil
}

func selectImmutableRelease(root *os.Root) (selectedRelease, error) {
	pointer, err := root.Lstat("current/active")
	if err != nil || pointer.Mode()&os.ModeSymlink == 0 {
		return selectedRelease{}, ErrInvalid
	}
	target, err := root.Readlink("current/active")
	cleanTarget := filepath.Clean(target)
	if err != nil || filepath.IsAbs(target) || cleanTarget != target || filepath.Dir(cleanTarget) != filepath.Join("..", "releases") {
		return selectedRelease{}, ErrInvalid
	}
	id := filepath.Base(cleanTarget)
	if id == "." || id == ".." || id == "" {
		return selectedRelease{}, ErrInvalid
	}
	releaseDir := filepath.Join("releases", id)
	info, err := root.Lstat(releaseDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return selectedRelease{}, ErrInvalid
	}
	return selectedRelease{target: target, releaseDir: releaseDir, pointer: pointer}, nil
}

func verifyRelease(ctx context.Context, rootDir string, selected selectedRelease, root *os.Root) (catalogEntry, *os.File, error) {
	manifestInfo, err := regularFileInfo(root, "release.json")
	if err != nil {
		return catalogEntry{}, nil, err
	}
	manifest, err := root.Open("release.json")
	if err != nil {
		return catalogEntry{}, nil, ErrUnavailable
	}
	var release Release
	decoder := json.NewDecoder(manifest)
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&release)
	endErr := ensureJSONEnd(decoder)
	manifestAfter, statErr := manifest.Stat()
	_ = manifest.Close()
	if decodeErr != nil || endErr != nil || statErr != nil || !sameStat(manifestInfo, manifestAfter) || !release.validManifest() {
		return catalogEntry{}, nil, ErrInvalid
	}

	artifactInfo, err := regularFileInfo(root, release.Filename)
	if err != nil {
		return catalogEntry{}, nil, err
	}
	file, err := root.Open(release.Filename)
	if err != nil {
		return catalogEntry{}, nil, ErrUnavailable
	}
	valid := false
	defer func() {
		if !valid {
			_ = file.Close()
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil || !sameStat(artifactInfo, openedInfo) || openedInfo.Size() != release.SizeBytes {
		return catalogEntry{}, nil, ErrInvalid
	}
	digest, err := hashWithContext(ctx, file)
	if err != nil {
		return catalogEntry{}, nil, err
	}
	afterHash, err := file.Stat()
	if err != nil {
		return catalogEntry{}, nil, ErrUnavailable
	}
	if !sameStat(openedInfo, afterHash) || hex.EncodeToString(digest) != release.SHA256 {
		return catalogEntry{}, nil, ErrInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return catalogEntry{}, nil, ErrUnavailable
	}
	release.rootDir = rootDir
	release.releaseDir = selected.releaseDir
	valid = true
	return catalogEntry{
		release: release, target: selected.target, pointer: selected.pointer,
		manifest: manifestAfter, artifact: afterHash,
	}, file, nil
}

func regularFileInfo(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, ErrInvalid
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalid
	}
	return info, nil
}

func sameStat(left, right os.FileInfo) bool {
	return left != nil && right != nil && os.SameFile(left, right) && left.Mode() == right.Mode() &&
		left.Size() == right.Size() && left.ModTime().Equal(right.ModTime())
}

func hashWithContext(ctx context.Context, reader io.Reader) ([]byte, error) {
	digest := sha256.New()
	buffer := make([]byte, 128*1024)
	if err := copyHash(ctx, digest, reader, buffer); err != nil {
		return nil, err
	}
	return digest.Sum(nil), nil
}

func copyHash(ctx context.Context, destination hash.Hash, source io.Reader, buffer []byte) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, err := source.Read(buffer)
		if read > 0 {
			_, _ = destination.Write(buffer[:read])
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return ErrUnavailable
		}
	}
}
