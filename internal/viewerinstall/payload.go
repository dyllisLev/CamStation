package viewerinstall

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxPayloadBytes int64 = 4 * 1024 * 1024 * 1024

type PayloadFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type PayloadManifest struct {
	SchemaVersion int           `json:"schemaVersion"`
	Version       string        `json:"version"`
	Digest        string        `json:"digest"`
	Files         []PayloadFile `json:"files"`
}

type PayloadDefaults struct {
	ServerURL                string `json:"serverUrl"`
	DisplayName              string `json:"displayName"`
	AllowDevelopmentUnsigned bool   `json:"allowDevelopmentUnsigned"`
	SignerThumbprint         string `json:"signerThumbprint,omitempty"`
}

func ExtractPayload(reader io.ReaderAt, size int64, destination string) (PayloadManifest, error) {
	if size <= 0 || size > maxPayloadBytes || !filepath.IsAbs(destination) {
		return PayloadManifest{}, errors.New("invalid payload archive")
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return PayloadManifest{}, err
	}
	entries := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		name, err := safePayloadPath(file.Name)
		if err != nil {
			return PayloadManifest{}, err
		}
		if _, exists := entries[name]; exists {
			return PayloadManifest{}, errors.New("payload contains duplicate path")
		}
		entries[name] = file
	}
	manifestFile := entries["manifest.json"]
	if manifestFile == nil || manifestFile.UncompressedSize64 > 1024*1024 {
		return PayloadManifest{}, errors.New("payload manifest missing or too large")
	}
	manifestReader, err := manifestFile.Open()
	if err != nil {
		return PayloadManifest{}, err
	}
	var manifest PayloadManifest
	decoder := json.NewDecoder(io.LimitReader(manifestReader, 1024*1024+1))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&manifest)
	closeErr := manifestReader.Close()
	if err != nil || closeErr != nil || manifest.SchemaVersion != SchemaVersion || !validVersion(manifest.Version) || !validDigest(manifest.Digest) || len(manifest.Files) == 0 || len(manifest.Files) > 20000 {
		return PayloadManifest{}, errors.New("invalid payload manifest")
	}
	listed := map[string]struct{}{"manifest.json": {}}
	for _, expected := range manifest.Files {
		name, err := safePayloadPath(expected.Path)
		if err != nil || name == "manifest.json" || expected.Size < 0 || expected.Size > maxPayloadBytes || !validDigest(expected.SHA256) {
			return PayloadManifest{}, errors.New("invalid payload file entry")
		}
		if _, duplicate := listed[name]; duplicate {
			return PayloadManifest{}, errors.New("duplicate payload manifest path")
		}
		listed[name] = struct{}{}
		file := entries[name]
		if file == nil || file.FileInfo().IsDir() || int64(file.UncompressedSize64) != expected.Size {
			return PayloadManifest{}, fmt.Errorf("payload file mismatch: %s", name)
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if err := extractVerifiedFile(file, target, expected); err != nil {
			return PayloadManifest{}, err
		}
	}
	if len(listed) != len(entries) {
		return PayloadManifest{}, errors.New("payload contains unlisted files")
	}
	return manifest, nil
}

func extractVerifiedFile(file *zip.File, target string, expected PayloadFile) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	temp, err := os.CreateTemp(filepath.Dir(target), ".payload-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(reader, expected.Size+1))
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != expected.Size || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return fmt.Errorf("payload verification failed: %s", expected.Path)
	}
	return os.Rename(tempPath, target)
}

func safePayloadPath(value string) (string, error) {
	if value == "" || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") || strings.ContainsRune(value, 0) {
		return "", errors.New("unsafe payload path")
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != value {
		return "", errors.New("unsafe payload path")
	}
	return cleaned, nil
}
