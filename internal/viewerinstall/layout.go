package viewerinstall

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const SchemaVersion = 1

type Layout struct {
	InstallDir string
	StateDir   string
}

func DefaultLayout() Layout {
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = filepath.FromSlash("/opt")
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = filepath.FromSlash("/var/lib")
	}
	return Layout{
		InstallDir: filepath.Join(programFiles, "CamStation Viewer"),
		StateDir:   filepath.Join(programData, "CamStation", "Viewer"),
	}
}

func (layout Layout) Validate() error {
	if !filepath.IsAbs(layout.InstallDir) || !filepath.IsAbs(layout.StateDir) {
		return errors.New("absolute install and state directories are required")
	}
	if filepath.Clean(layout.InstallDir) == filepath.Clean(layout.StateDir) {
		return errors.New("install and state directories must differ")
	}
	return nil
}

func (layout Layout) Ensure() error {
	if err := layout.Validate(); err != nil {
		return err
	}
	for _, dir := range []string{layout.InstallDir, layout.ReleaseRoot(), layout.StagingRoot(), layout.BackupRoot(), layout.StateDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (layout Layout) ReleaseRoot() string         { return filepath.Join(layout.InstallDir, "releases") }
func (layout Layout) ReleaseDir(id string) string { return filepath.Join(layout.ReleaseRoot(), id) }
func (layout Layout) CurrentPath() string         { return filepath.Join(layout.InstallDir, "current.json") }
func (layout Layout) JournalPath() string         { return filepath.Join(layout.StateDir, "transaction.json") }
func (layout Layout) LockPath() string            { return filepath.Join(layout.StateDir, "update.lock") }
func (layout Layout) StagingRoot() string         { return filepath.Join(layout.StateDir, "staging") }
func (layout Layout) BackupRoot() string          { return filepath.Join(layout.StateDir, "backups") }
func (layout Layout) TransactionBackup(id string) string {
	return filepath.Join(layout.BackupRoot(), id)
}

type Current struct {
	SchemaVersion int    `json:"schemaVersion"`
	ReleaseID     string `json:"releaseId"`
	Version       string `json:"version"`
	Digest        string `json:"digest"`
	AgentPath     string `json:"agentPath"`
	ViewerPath    string `json:"viewerPath"`
}

func currentFor(release Release) Current {
	base := filepath.Join("releases", release.ReleaseID)
	return Current{
		SchemaVersion: SchemaVersion,
		ReleaseID:     release.ReleaseID,
		Version:       release.Version,
		Digest:        release.Digest,
		AgentPath:     filepath.Join(base, "camstation-viewer-agent.exe"),
		ViewerPath:    filepath.Join(base, "viewer", "CamStationViewer.exe"),
	}
}

func LoadCurrent(layout Layout) (Current, error) {
	var current Current
	if err := readJSON(layout.CurrentPath(), &current); err != nil {
		return Current{}, err
	}
	if current.SchemaVersion != SchemaVersion || current.ReleaseID == "" || current.Version == "" || !validDigest(current.Digest) {
		return Current{}, errors.New("invalid current release pointer")
	}
	if current.ReleaseID != ReleaseID(current.Version, current.Digest) {
		return Current{}, errors.New("current release identity mismatch")
	}
	return current, nil
}

func SaveCurrent(layout Layout, current Current) error {
	current.SchemaVersion = SchemaVersion
	if current.AgentPath == "" || current.ViewerPath == "" {
		current = currentFor(Release{Version: current.Version, Digest: current.Digest, ReleaseID: current.ReleaseID})
	}
	if current.ReleaseID != ReleaseID(current.Version, current.Digest) {
		return errors.New("invalid current release identity")
	}
	return atomicWriteJSON(layout.CurrentPath(), current)
}

func ReleaseID(version, digest string) string {
	if !validVersion(version) || !validDigest(digest) {
		return ""
	}
	return version + "-" + strings.ToLower(digest)
}

func validVersion(value string) bool {
	if value == "" || len(value) > 64 || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._-", char)) {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func atomicWriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if runtime.GOOS == "windows" {
		return closeErr
	}
	if err != nil {
		return err
	}
	return closeErr
}

func readJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return errors.New("invalid bounded JSON file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("JSON file contains trailing data")
	}
	return nil
}

func copyTree(source, destination string) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("release source escaped root")
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("release contains non-regular file %s", relative)
		}
		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(targetFile, sourceFile)
		if copyErr == nil {
			copyErr = targetFile.Sync()
		}
		closeErr := targetFile.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func InstallStablePayload(layout Layout, payloadDir, setupPath string) error {
	if err := layout.Ensure(); err != nil {
		return err
	}
	files := [][2]string{
		{filepath.Join(payloadDir, "stable", "CamStationViewerHost.exe"), stableHostPath(layout)},
		{filepath.Join(payloadDir, "stable", "CamStationViewerBootstrap.exe"), stableBootstrapPath(layout)},
		{setupPath, filepath.Join(layout.InstallDir, "CamStationViewerSetup.exe")},
		{setupPath, stableUpdaterPath(layout)},
	}
	for _, pair := range files {
		if err := copyFileAtomic(pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

func copyFileAtomic(source, destination string) error {
	source = filepath.Clean(source)
	destination = filepath.Clean(destination)
	if source == destination {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("stable payload source is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".stable-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, input); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return err
	}
	return syncDir(filepath.Dir(destination))
}
