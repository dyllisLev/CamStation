package viewerrelease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrUnavailable = errors.New("viewer release unavailable")
	ErrInvalid     = errors.New("viewer release invalid")
)

type Release struct {
	Version             string    `json:"version"`
	Filename            string    `json:"filename"`
	SizeBytes           int64     `json:"sizeBytes"`
	SHA256              string    `json:"sha256"`
	PublishedAt         time.Time `json:"publishedAt"`
	DevelopmentUnsigned bool      `json:"developmentUnsigned"`
	dir                 string
}

func Load(dir string) (Release, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Release{}, ErrUnavailable
	}
	defer root.Close()
	manifest, err := root.Open("release.json")
	if err != nil {
		return Release{}, ErrUnavailable
	}
	defer manifest.Close()

	var release Release
	decoder := json.NewDecoder(manifest)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&release); err != nil {
		return Release{}, ErrInvalid
	}
	if err := ensureJSONEnd(decoder); err != nil || !release.validManifest() {
		return Release{}, ErrInvalid
	}
	release.dir = dir

	file, err := release.OpenVerified()
	if err != nil {
		return Release{}, err
	}
	_ = file.Close()
	return release, nil
}

func (r Release) DownloadURL() string {
	return "/api/viewers/app/download"
}

func (r Release) OpenVerified() (*os.File, error) {
	if !r.validManifest() {
		return nil, ErrInvalid
	}
	root, err := os.OpenRoot(r.dir)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer root.Close()
	file, err := root.Open(r.Filename)
	if err != nil {
		return nil, ErrUnavailable
	}
	valid := false
	defer func() {
		if !valid {
			_ = file.Close()
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, ErrUnavailable
	}
	if !info.Mode().IsRegular() || info.Size() != r.SizeBytes {
		return nil, ErrInvalid
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, ErrUnavailable
	}
	if hex.EncodeToString(hash.Sum(nil)) != r.SHA256 {
		return nil, ErrInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrUnavailable
	}
	valid = true
	return file, nil
}

func (r Release) validManifest() bool {
	if r.Filename == "" || filepath.IsAbs(r.Filename) || strings.ContainsAny(r.Filename, `/\`) || filepath.Ext(r.Filename) != ".exe" {
		return false
	}
	if r.SizeBytes <= 0 || len(r.SHA256) != sha256.Size*2 || strings.ToLower(r.SHA256) != r.SHA256 {
		return false
	}
	_, err := hex.DecodeString(r.SHA256)
	return err == nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}
