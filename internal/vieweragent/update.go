package vieweragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxInstallerBytes int64 = 4 * 1024 * 1024 * 1024

var (
	ErrUpdateLaunched   = errors.New("update installer launched")
	ErrUpdateHardReject = errors.New("update hard rejected")
)

type ReleaseMetadata struct {
	Version             string `json:"version"`
	Filename            string `json:"filename"`
	SizeBytes           int64  `json:"sizeBytes"`
	SHA256              string `json:"sha256"`
	DevelopmentUnsigned bool   `json:"developmentUnsigned"`
	SignerThumbprint    string `json:"signerThumbprint,omitempty"`
	DownloadURL         string `json:"downloadUrl"`
}

type UpdateTarget struct {
	Version       string
	SHA256        string
	Generation    int64
	TransactionID string
}

type UpdateRunner struct {
	HTTPClient               *http.Client
	ServerURL                string
	StateDir                 string
	AllowDevelopmentUnsigned bool
	ExpectedSignerThumbprint string
	VerifySignature          func(path, thumbprint string, allowUnsigned bool) error
	WaitViewerReady          func(context.Context, time.Duration) error
	LaunchDetached           func(path string, args []string) error
	Sleep                    func(context.Context, time.Duration) error
}

func (runner UpdateRunner) Run(ctx context.Context, target UpdateTarget) error {
	serverURL, err := ValidateServerURL(runner.ServerURL)
	if err != nil || !validUpdateTarget(target) || !filepath.IsAbs(runner.StateDir) {
		return errors.New("invalid update target configuration")
	}
	journalPath := filepath.Join(runner.StateDir, "update.json")
	journal, err := LoadUpdateJournal(journalPath)
	if err != nil {
		return err
	}
	if journal.IsQuarantined(target.Version, target.SHA256, target.Generation) {
		return ErrUpdateHardReject
	}
	if journal.TargetVersion != target.Version || !strings.EqualFold(journal.ArtifactSHA256, target.SHA256) || journal.Generation != target.Generation {
		journal.DownloadAttempts = 0
	}
	journal.TargetVersion = target.Version
	journal.ArtifactSHA256 = target.SHA256
	journal.Generation = target.Generation
	journal.TransactionID = target.TransactionID
	journal.State = "checking_release"
	if err := SaveUpdateJournal(journalPath, journal); err != nil {
		return err
	}

	metadata, err := runner.loadMetadata(ctx, serverURL)
	if err != nil {
		return err
	}
	if err := validateMetadata(metadata, target, runner.AllowDevelopmentUnsigned, runner.ExpectedSignerThumbprint); err != nil {
		return runner.reject(journalPath, journal, target, "metadata_mismatch", err)
	}

	stageDir := filepath.Join(runner.StateDir, "updates", target.Version+"-"+target.SHA256+"-"+strconv.FormatInt(target.Generation, 10))
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return err
	}
	installerPath := filepath.Join(stageDir, "CamStationViewerSetup.exe")
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}
	staged, stageErr := verifyStagedInstaller(installerPath, metadata)
	if stageErr != nil {
		return runner.reject(journalPath, journal, target, "staged_installer_invalid", stageErr)
	}
	if !staged && journal.DownloadAttempts >= len(delays)+1 {
		return errors.New("download retry ledger is exhausted")
	}
	for !staged && journal.DownloadAttempts < len(delays)+1 {
		if journal.NextAttemptAt != nil && time.Now().Before(*journal.NextAttemptAt) {
			sleep := runner.Sleep
			if sleep == nil {
				sleep = waitContext
			}
			if err := sleep(ctx, time.Until(*journal.NextAttemptAt)); err != nil {
				return err
			}
			journal.NextAttemptAt = nil
			if err := SaveUpdateJournal(journalPath, journal); err != nil {
				return err
			}
		}
		journal.DownloadAttempts++
		journal.NextAttemptAt = nil
		journal.State = "downloading"
		if err := SaveUpdateJournal(journalPath, journal); err != nil {
			return err
		}
		err = runner.download(ctx, serverURL, metadata, installerPath)
		if err == nil {
			staged = true
			break
		}
		var hard *hardUpdateError
		if errors.As(err, &hard) {
			return runner.reject(journalPath, journal, target, hard.category, hard)
		}
		if journal.DownloadAttempts == len(delays)+1 {
			journal.State = "download_failed"
			journal.LastError = "download_failed"
			_ = SaveUpdateJournal(journalPath, journal)
			return fmt.Errorf("download retries exhausted: %w", err)
		}
		journal.State = "download_retry_wait"
		delay := delays[journal.DownloadAttempts-1]
		next := time.Now().UTC().Add(delay)
		journal.NextAttemptAt = &next
		if err := SaveUpdateJournal(journalPath, journal); err != nil {
			return err
		}
		sleep := runner.Sleep
		if sleep == nil {
			sleep = waitContext
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		journal.NextAttemptAt = nil
		if err := SaveUpdateJournal(journalPath, journal); err != nil {
			return err
		}
	}
	journal.NextAttemptAt = nil
	journal.State = "verified"
	journal.InstallerPath = installerPath
	if err := SaveUpdateJournal(journalPath, journal); err != nil {
		return err
	}
	verify := runner.VerifySignature
	if verify == nil {
		verify = verifyAuthenticode
	}
	if err := verify(installerPath, metadata.SignerThumbprint, metadata.DevelopmentUnsigned && runner.AllowDevelopmentUnsigned); err != nil {
		return runner.reject(journalPath, journal, target, "signature_invalid", err)
	}
	waitReady := runner.WaitViewerReady
	if waitReady == nil {
		return errors.New("Viewer-ready gate is required")
	}
	journal.State = "waiting_for_viewer_session"
	if err := SaveUpdateJournal(journalPath, journal); err != nil {
		return err
	}
	if err := waitReady(ctx, 30*time.Second); err != nil {
		return err
	}
	launch := runner.LaunchDetached
	if launch == nil {
		launch = launchUpdateDetached
	}
	args := []string{
		"--update", "--transaction-id", target.TransactionID,
		"--generation", strconv.FormatInt(target.Generation, 10),
		"--expected-sha", target.SHA256,
		"--parent-pid", strconv.Itoa(os.Getpid()),
	}
	journal.State = "launching_installer"
	if err := SaveUpdateJournal(journalPath, journal); err != nil {
		return err
	}
	if err := launch(installerPath, args); err != nil {
		journal.State = "launch_failed"
		journal.LastError = "launch_failed"
		_ = SaveUpdateJournal(journalPath, journal)
		return err
	}
	journal.State = "installer_launched"
	journal.LastError = ""
	if err := SaveUpdateJournal(journalPath, journal); err != nil {
		return err
	}
	return ErrUpdateLaunched
}

func verifyStagedInstaller(path string, metadata ReleaseMetadata) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != metadata.SizeBytes {
		return false, errors.New("staged installer size is invalid")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != metadata.SHA256 {
		return false, errors.New("staged installer SHA-256 is invalid")
	}
	return true, nil
}

func (runner UpdateRunner) loadMetadata(ctx context.Context, serverURL string) (ReleaseMetadata, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/viewers/app/version", nil)
	if err != nil {
		return ReleaseMetadata{}, err
	}
	response, err := runner.client().Do(request)
	if err != nil {
		return ReleaseMetadata{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ReleaseMetadata{}, fmt.Errorf("release metadata status %s", response.Status)
	}
	var metadata ReleaseMetadata
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err := decoder.Decode(&metadata); err != nil {
		return ReleaseMetadata{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ReleaseMetadata{}, errors.New("release metadata contains trailing data")
	}
	return metadata, nil
}

func (runner UpdateRunner) download(ctx context.Context, serverURL string, metadata ReleaseMetadata, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/viewers/app/download", nil)
	if err != nil {
		return err
	}
	response, err := runner.client().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("release download status %s", response.Status)
	}
	if response.ContentLength >= 0 && response.ContentLength != metadata.SizeBytes {
		return &hardUpdateError{category: "size_mismatch", err: errors.New("installer content length mismatch")}
	}
	temp := destination + ".part"
	_ = os.Remove(temp)
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, metadata.SizeBytes+1))
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != metadata.SizeBytes {
		return &hardUpdateError{category: "size_mismatch", err: errors.New("installer size mismatch")}
	}
	if hex.EncodeToString(hash.Sum(nil)) != metadata.SHA256 {
		return &hardUpdateError{category: "hash_mismatch", err: errors.New("installer SHA-256 mismatch")}
	}
	if err := os.Rename(temp, destination); err != nil {
		return err
	}
	return nil
}

func (runner UpdateRunner) reject(path string, journal UpdateJournal, target UpdateTarget, category string, cause error) error {
	journal.State = "rejected"
	journal.LastError = category
	journal.Quarantine(target.Version, target.SHA256, target.Generation, time.Now().UTC(), category)
	if err := SaveUpdateJournal(path, journal); err != nil {
		return errors.Join(ErrUpdateHardReject, cause, err)
	}
	return errors.Join(ErrUpdateHardReject, cause)
}

func (runner UpdateRunner) client() *http.Client {
	if runner.HTTPClient != nil {
		return runner.HTTPClient
	}
	return http.DefaultClient
}

type hardUpdateError struct {
	category string
	err      error
}

func (err *hardUpdateError) Error() string { return err.err.Error() }
func (err *hardUpdateError) Unwrap() error { return err.err }

func validateMetadata(metadata ReleaseMetadata, target UpdateTarget, allowDevelopmentUnsigned bool, expectedSignerThumbprint string) error {
	if metadata.Version != target.Version || metadata.SHA256 != target.SHA256 || metadata.Filename != "CamStationViewerSetup.exe" ||
		metadata.DownloadURL != "/api/viewers/app/download" || metadata.SizeBytes <= 0 || metadata.SizeBytes > maxInstallerBytes {
		return errors.New("release metadata does not match update command")
	}
	if metadata.DevelopmentUnsigned {
		if !allowDevelopmentUnsigned || metadata.SignerThumbprint != "" {
			return errors.New("development unsigned release is not allowed")
		}
		return nil
	}
	if !validThumbprint(metadata.SignerThumbprint) {
		return errors.New("signed release thumbprint is missing")
	}
	if !validThumbprint(expectedSignerThumbprint) || !strings.EqualFold(metadata.SignerThumbprint, expectedSignerThumbprint) {
		return errors.New("release signer does not match installer trust")
	}
	return nil
}

func validUpdateTarget(target UpdateTarget) bool {
	if target.Generation <= 0 || target.TransactionID == "" || len(target.TransactionID) > 128 || target.Version == "" || len(target.Version) > 64 || len(target.SHA256) != 64 || target.SHA256 != strings.ToLower(target.SHA256) {
		return false
	}
	if _, err := hex.DecodeString(target.SHA256); err != nil {
		return false
	}
	for _, value := range []string{target.TransactionID, target.Version} {
		for _, char := range value {
			if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._-", char)) {
				return false
			}
		}
	}
	return true
}

func validThumbprint(value string) bool {
	value = strings.ToLower(value)
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
