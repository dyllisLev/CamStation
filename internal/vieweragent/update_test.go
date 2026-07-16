package vieweragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"camstation/internal/viewerinstall"
)

func TestUpdateRunnerDownloadsVerifiesAndLaunchesFixedSameOriginInstaller(t *testing.T) {
	payload := []byte("MZ production installer")
	digest := sha256Hex(payload)
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/viewers/app/version":
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.1", int64(len(payload)), digest, true))
		case "/api/viewers/app/download":
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var launched string
	var args []string
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: t.TempDir(), AllowDevelopmentUnsigned: true,
		VerifySignature: func(path, thumbprint string, allowUnsigned bool) error {
			if thumbprint != "" || !allowUnsigned {
				t.Fatalf("thumbprint=%q allowUnsigned=%v", thumbprint, allowUnsigned)
			}
			return nil
		},
		WaitViewerReady: func(context.Context, time.Duration) error { return nil },
		LaunchDetached: func(path string, launchArgs []string) error {
			launched, args = path, append([]string(nil), launchArgs...)
			return nil
		},
	}
	target := UpdateTarget{Version: "2.0.1", SHA256: digest, Generation: 9, TransactionID: "update-9", CommandID: 41, PayloadHash: "payload-41"}
	if err := runner.Run(t.Context(), target); !errors.Is(err, ErrUpdateLaunched) {
		t.Fatalf("Run error=%v", err)
	}
	if !reflect.DeepEqual(requested, []string{"/api/viewers/app/version", "/api/viewers/app/download"}) {
		t.Fatalf("unexpected endpoints: %v", requested)
	}
	data, err := os.ReadFile(launched)
	if err != nil || !reflect.DeepEqual(data, payload) {
		t.Fatalf("staged installer=%q err=%v", data, err)
	}
	wantArgs := []string{"--update", "--transaction-id", "update-9", "--command-id", "41", "--payload-hash", "payload-41", "--generation", "9", "--expected-sha", digest, "--parent-pid"}
	if len(args) != len(wantArgs)+1 || !reflect.DeepEqual(args[:len(wantArgs)], wantArgs) {
		t.Fatalf("launch args=%v", args)
	}
	journal, err := LoadUpdateJournal(filepath.Join(runner.StateDir, "update.json"))
	if err != nil || journal.State != "launching_installer" || journal.DownloadAttempts != 1 || journal.TransactionID != target.TransactionID || journal.CommandID != target.CommandID || journal.PayloadHash != target.PayloadHash {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestUpdateRunnerRejectsMetadataOrContentMismatchWithoutRetry(t *testing.T) {
	payload := []byte("MZ altered")
	expected := sha256Hex([]byte("MZ expected"))
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.1", int64(len(payload)), expected, true))
			return
		}
		downloads++
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	launched := false
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: t.TempDir(), AllowDevelopmentUnsigned: true,
		VerifySignature: func(string, string, bool) error { return nil },
		WaitViewerReady: func(context.Context, time.Duration) error { return nil },
		LaunchDetached:  func(string, []string) error { launched = true; return nil },
		Sleep:           func(context.Context, time.Duration) error { t.Fatal("hard verification failure retried"); return nil },
	}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.1", SHA256: expected, Generation: 1, TransactionID: "tx-1"})
	if !errors.Is(err, ErrUpdateHardReject) || downloads != 1 || launched {
		t.Fatalf("err=%v downloads=%d launched=%v", err, downloads, launched)
	}
	journal, _ := LoadUpdateJournal(filepath.Join(runner.StateDir, "update.json"))
	if !journal.IsQuarantined("2.0.1", expected, 1) || journal.State != "rejected" {
		t.Fatalf("hard rejection was not durable: %+v", journal)
	}
}

func TestUpdateRunnerPersistsBoundedDownloadRetryLedger(t *testing.T) {
	payload := []byte("MZ installer")
	digest := sha256Hex(payload)
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.2", int64(len(payload)), digest, true))
			return
		}
		downloads++
		if downloads < 4 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	var delays []time.Duration
	var runner UpdateRunner
	runner = UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: t.TempDir(), AllowDevelopmentUnsigned: true,
		VerifySignature: func(string, string, bool) error { return nil },
		WaitViewerReady: func(context.Context, time.Duration) error { return nil },
		LaunchDetached:  func(string, []string) error { return nil },
		Sleep: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			journal, err := LoadUpdateJournal(filepath.Join(runner.StateDir, "update.json"))
			if err != nil || journal.DownloadAttempts != len(delays) {
				t.Fatalf("attempt ledger not durable before delay: %+v err=%v", journal, err)
			}
			return nil
		},
	}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.2", SHA256: digest, Generation: 3, TransactionID: "tx-3"})
	if !errors.Is(err, ErrUpdateLaunched) || downloads != 4 {
		t.Fatalf("err=%v downloads=%d", err, downloads)
	}
	if !reflect.DeepEqual(delays, []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}) {
		t.Fatalf("retry delays=%v", delays)
	}
}

func TestUpdateRunnerDoesNotResetRetryBudgetAfterRestart(t *testing.T) {
	payload := []byte("MZ installer")
	digest := sha256Hex(payload)
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.2", int64(len(payload)), digest, true))
			return
		}
		downloads++
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	stateDir := t.TempDir()
	journal := UpdateJournal{TargetVersion: "2.0.2", ArtifactSHA256: digest, Generation: 3, TransactionID: "tx-3", DownloadAttempts: 3}
	if err := SaveUpdateJournal(filepath.Join(stateDir, "update.json"), journal); err != nil {
		t.Fatal(err)
	}
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: stateDir, AllowDevelopmentUnsigned: true,
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("exhausted ledger slept for another retry")
			return nil
		},
	}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.2", SHA256: digest, Generation: 3, TransactionID: "tx-3"})
	if err == nil || downloads != 1 {
		t.Fatalf("err=%v downloads=%d", err, downloads)
	}
	journal, _ = LoadUpdateJournal(filepath.Join(stateDir, "update.json"))
	if journal.DownloadAttempts != 4 {
		t.Fatalf("durable attempts=%d", journal.DownloadAttempts)
	}
}

func TestUpdateRunnerReusesVerifiedStageAfterRestart(t *testing.T) {
	payload := []byte("MZ verified stage")
	digest := sha256Hex(payload)
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.8", int64(len(payload)), digest, true))
			return
		}
		downloads++
		http.Error(w, "must not download", http.StatusInternalServerError)
	}))
	defer server.Close()
	stateDir := t.TempDir()
	stageDir := filepath.Join(stateDir, "updates", "2.0.8-"+digest+"-8")
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	installer := filepath.Join(stageDir, "CamStationViewerSetup.exe")
	if err := os.WriteFile(installer, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := UpdateJournal{State: "verified", TargetVersion: "2.0.8", ArtifactSHA256: digest, Generation: 8, TransactionID: "tx-8", DownloadAttempts: 1, InstallerPath: installer}
	if err := SaveUpdateJournal(filepath.Join(stateDir, "update.json"), journal); err != nil {
		t.Fatal(err)
	}
	launched := false
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: stateDir, AllowDevelopmentUnsigned: true,
		VerifySignature: func(string, string, bool) error { return nil },
		WaitViewerReady: func(context.Context, time.Duration) error { return nil },
		LaunchDetached:  func(path string, _ []string) error { launched = path == installer; return nil },
		Sleep:           func(context.Context, time.Duration) error { return errors.New("unexpected retry") },
	}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.8", SHA256: digest, Generation: 8, TransactionID: "tx-8"})
	if !errors.Is(err, ErrUpdateLaunched) || !launched || downloads != 0 {
		t.Fatalf("err=%v launched=%v downloads=%d", err, launched, downloads)
	}
}

func TestUpdateRunnerBoundsMetadataResponseHeaderBlackhole(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	digest := sha256Hex([]byte("setup"))
	stateDir := t.TempDir()
	var delays []time.Duration
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: stateDir,
		MetadataDeadline: 20 * time.Millisecond,
		Sleep:            func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil },
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	err := runner.Run(ctx, UpdateTarget{Version: "2.0.9", SHA256: digest, Generation: 9, TransactionID: "tx-9"})
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 4 {
		t.Fatalf("err=%v requests=%d", err, requests.Load())
	}
	journal, loadErr := LoadUpdateJournal(filepath.Join(stateDir, "update.json"))
	if loadErr != nil || journal.MetadataAttempts != 4 || journal.State != "metadata_failed" {
		t.Fatalf("journal=%+v err=%v", journal, loadErr)
	}
	if !reflect.DeepEqual(delays, []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}) {
		t.Fatalf("delays=%v", delays)
	}
}

func TestUpdateRunnerDoesNotResetMetadataRetryBudgetAfterRestart(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	digest := sha256Hex([]byte("setup"))
	stateDir := t.TempDir()
	journal := UpdateJournal{
		State: "metadata_retry_wait", TargetVersion: "2.0.9", ArtifactSHA256: digest,
		Generation: 9, TransactionID: "tx-9", MetadataAttempts: 3,
	}
	if err := SaveUpdateJournal(filepath.Join(stateDir, "update.json"), journal); err != nil {
		t.Fatal(err)
	}
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: stateDir,
		MetadataDeadline: 20 * time.Millisecond,
		Sleep: func(context.Context, time.Duration) error {
			t.Fatal("exhausted metadata ledger slept for another retry")
			return nil
		},
	}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.9", SHA256: digest, Generation: 9, TransactionID: "tx-9"})
	if err == nil || requests.Load() != 1 {
		t.Fatalf("err=%v requests=%d", err, requests.Load())
	}
	journal, err = LoadUpdateJournal(filepath.Join(stateDir, "update.json"))
	if err != nil || journal.MetadataAttempts != 4 || journal.State != "metadata_failed" {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestUpdateRunnerBoundsStalledInstallerBodyPerAttempt(t *testing.T) {
	payload := []byte("MZ complete setup")
	digest := sha256Hex(payload)
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.10", int64(len(payload)), digest, true))
			return
		}
		downloads.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload[:1])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	stateDir := t.TempDir()
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: stateDir, AllowDevelopmentUnsigned: true,
		DownloadDeadline: 20 * time.Millisecond,
		Sleep:            func(context.Context, time.Duration) error { return nil },
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	err := runner.Run(ctx, UpdateTarget{Version: "2.0.10", SHA256: digest, Generation: 10, TransactionID: "tx-10"})
	if !errors.Is(err, context.DeadlineExceeded) || downloads.Load() != 4 {
		t.Fatalf("err=%v downloads=%d", err, downloads.Load())
	}
	journal, loadErr := LoadUpdateJournal(filepath.Join(stateDir, "update.json"))
	if loadErr != nil || journal.DownloadAttempts != 4 || journal.State != "download_failed" {
		t.Fatalf("journal=%+v err=%v", journal, loadErr)
	}
}

func TestUpdateRunnerRequiresExplicitDevelopmentUnsignedPolicy(t *testing.T) {
	payload := []byte("MZ unsigned")
	digest := sha256Hex(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.3", int64(len(payload)), digest, true))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	runner := UpdateRunner{HTTPClient: server.Client(), ServerURL: server.URL, StateDir: t.TempDir()}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.3", SHA256: digest, Generation: 1, TransactionID: "tx-u"})
	if !errors.Is(err, ErrUpdateHardReject) {
		t.Fatalf("development unsigned installer accepted: %v", err)
	}
}

func TestUpdateRunnerRequiresInstallerOwnedSignerThumbprint(t *testing.T) {
	payload := []byte("MZ signed")
	digest := sha256Hex(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			metadata := releaseMetadata("2.0.3", int64(len(payload)), digest, false)
			metadata.SignerThumbprint = strings.Repeat("b", 40)
			_ = json.NewEncoder(w).Encode(metadata)
			return
		}
		t.Fatal("signer mismatch reached download")
	}))
	defer server.Close()
	runner := UpdateRunner{HTTPClient: server.Client(), ServerURL: server.URL, StateDir: t.TempDir(), ExpectedSignerThumbprint: strings.Repeat("a", 40)}
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.3", SHA256: digest, Generation: 1, TransactionID: "tx-s"})
	if !errors.Is(err, ErrUpdateHardReject) {
		t.Fatalf("untrusted signer accepted: %v", err)
	}
}

func TestUpdateRunnerWaitsForStableViewerBeforeLaunch(t *testing.T) {
	payload := []byte("MZ ready")
	digest := sha256Hex(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/app/version" {
			_ = json.NewEncoder(w).Encode(releaseMetadata("2.0.4", int64(len(payload)), digest, true))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	waited := time.Duration(0)
	launched := false
	runner := UpdateRunner{
		HTTPClient: server.Client(), ServerURL: server.URL, StateDir: t.TempDir(), AllowDevelopmentUnsigned: true,
		VerifySignature: func(string, string, bool) error { return nil },
		WaitViewerReady: func(_ context.Context, stable time.Duration) error { waited = stable; return nil },
		LaunchDetached:  func(string, []string) error { launched = true; return nil },
	}
	_ = runner.Run(t.Context(), UpdateTarget{Version: "2.0.4", SHA256: digest, Generation: 1, TransactionID: "tx-r"})
	if waited != 30*time.Second || !launched {
		t.Fatalf("stable wait=%v launched=%v", waited, launched)
	}
}

type updateExecutorFunc func(context.Context, UpdateTarget) error

func (function updateExecutorFunc) Run(ctx context.Context, target UpdateTarget) error {
	return function(ctx, target)
}

func TestDefaultAgentExecutesUpdateThroughProductionRunner(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	digest := sha256Hex([]byte("setup"))
	var received UpdateTarget
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(_ context.Context, target UpdateTarget) error {
		received = target
		return ErrUpdateLaunched
	})}
	command := Command{ID: 81, Type: "update_app", DesiredVersion: "2.0.5", ArtifactSHA256: digest, Generation: 12, PayloadHash: "update-payload", TTLSeconds: 300}
	record, err := agent.HandleCommand(t.Context(), command)
	if !errors.Is(err, ErrAgentRestartRequested) || record.State != CommandRunning {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if received.Version != command.DesiredVersion || received.SHA256 != digest || received.Generation != 12 || received.TransactionID != record.OperationKey || received.CommandID != command.ID || received.PayloadHash != command.PayloadHash {
		t.Fatalf("update target=%+v record=%+v", received, record)
	}
}

func TestReconcileResumesInterruptedUpdateBeforeInstallerLaunch(t *testing.T) {
	dir := t.TempDir()
	digest := sha256Hex([]byte("setup"))
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	record := CommandRecord{ID: 82, Type: "update_app", PayloadHash: "p", OperationKey: "update-82", DesiredVersion: "2.1.0", ArtifactSHA256: digest, Generation: 4, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"82": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "waiting_for_viewer_session", TargetVersion: "2.1.0", ArtifactSHA256: digest, Generation: 4, TransactionID: "update-82"}); err != nil {
		t.Fatal(err)
	}
	called := 0
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(_ context.Context, target UpdateTarget) error {
		called++
		if target.TransactionID != "update-82" || target.SHA256 != digest {
			t.Fatalf("target=%+v", target)
		}
		return ErrUpdateLaunched
	})}
	_, err := agent.Reconcile(t.Context())
	if !errors.Is(err, ErrAgentRestartRequested) || called != 1 {
		t.Fatalf("err=%v calls=%d", err, called)
	}
}

func TestReconcileRelaunchesStaleInstallerStateWithoutTransactionClaim(t *testing.T) {
	dir := t.TempDir()
	digest := sha256Hex([]byte("setup"))
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	record := CommandRecord{ID: 83, Type: "update_app", PayloadHash: "p", OperationKey: "update-83", DesiredVersion: "2.1.1", ArtifactSHA256: digest, Generation: 5, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"83": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "installer_launched", TargetVersion: "2.1.1", ArtifactSHA256: digest, Generation: 5, TransactionID: "update-83"}); err != nil {
		t.Fatal(err)
	}
	called := 0
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(context.Context, UpdateTarget) error {
		called++
		return ErrUpdateLaunched
	})}
	_, err := agent.Reconcile(t.Context())
	if !errors.Is(err, ErrAgentRestartRequested) || called != 1 {
		t.Fatalf("err=%v calls=%d", err, called)
	}
}

func TestReconcileKeepsLiveExactOwnerThenCompletesAbandonedTransactionOnce(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	digest := sha256Hex([]byte("setup"))
	target := UpdateTarget{Version: "2.1.1", SHA256: digest, Generation: 5, TransactionID: "update-83"}
	source := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	injected := errors.New("stop after durable claim")
	manager := viewerinstall.Manager{
		Layout: layout, Registration: updateTestRegistration{},
		FailAfter: func(phase viewerinstall.Phase) error {
			if phase == viewerinstall.PhasePreparing {
				return injected
			}
			return nil
		},
	}
	request := viewerinstall.Request{TransactionID: target.TransactionID, Generation: target.Generation, SourceDir: source, Release: viewerinstall.Release{Version: target.Version, Digest: target.SHA256, ReleaseID: viewerinstall.ReleaseID(target.Version, target.SHA256)}}
	if err := manager.Apply(t.Context(), request); !errors.Is(err, injected) {
		t.Fatalf("Apply error=%v", err)
	}
	paths := MachinePaths{State: filepath.Join(layout.StateDir, "state.json"), Commands: filepath.Join(layout.StateDir, "commands.json"), Update: filepath.Join(layout.StateDir, "update.json")}
	record := CommandRecord{ID: 83, Type: "update_app", PayloadHash: "p", OperationKey: target.TransactionID, DesiredVersion: target.Version, ArtifactSHA256: digest, Generation: target.Generation, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"83": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "installer_launched", TargetVersion: target.Version, ArtifactSHA256: digest, Generation: target.Generation, TransactionID: target.TransactionID}); err != nil {
		t.Fatal(err)
	}
	liveOwner, err := viewerinstall.Acquire(layout)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	agent := Agent{Config: Config{InstallDir: layout.InstallDir}, Paths: paths, Updater: updateExecutorFunc(func(context.Context, UpdateTarget) error {
		called++
		if err := (viewerinstall.Manager{Layout: layout, Registration: updateTestRegistration{}}).Apply(t.Context(), request); err != nil {
			return err
		}
		matched, err := ReconcileCommittedUpdate(layout.StateDir)
		if err != nil || !matched {
			return fmt.Errorf("commit reconciliation matched=%v: %w", matched, err)
		}
		return nil
	})}
	results, err := agent.Reconcile(t.Context())
	if err != nil || len(results) != 0 || called != 0 {
		t.Fatalf("live results=%+v calls=%d err=%v", results, called, err)
	}
	if err := liveOwner.Close(); err != nil {
		t.Fatal(err)
	}
	results, err = agent.Reconcile(t.Context())
	if err != nil || len(results) != 1 || results[0].State != CommandSucceeded || called != 1 {
		t.Fatalf("abandoned results=%+v calls=%d err=%v", results, called, err)
	}
	results, err = agent.Reconcile(t.Context())
	if err != nil || len(results) != 0 || called != 1 {
		t.Fatalf("repeat results=%+v calls=%d err=%v", results, called, err)
	}
}

func TestAvailableTransactionOwnershipStaysHeldThroughCriticalSection(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withAvailableTransactionOwnership(layout, func(*viewerinstall.Ownership) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	contender, err := viewerinstall.Acquire(layout)
	if contender != nil {
		_ = contender.Close()
	}
	if !errors.Is(err, viewerinstall.ErrUpdateOwned) {
		t.Fatalf("critical section released transaction ownership: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	reacquired, err := viewerinstall.Acquire(layout)
	if err != nil {
		t.Fatalf("ownership was not released after critical section: %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonedHandoffOwnedRereadNeverDowngradesRacedCommit(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256Hex([]byte("setup"))
	target := UpdateTarget{Version: "2.1.1", SHA256: digest, Generation: 5, TransactionID: "update-83"}
	request := viewerinstall.Request{TransactionID: target.TransactionID, Generation: target.Generation, SourceDir: source, Release: viewerinstall.Release{Version: target.Version, Digest: target.SHA256, ReleaseID: viewerinstall.ReleaseID(target.Version, target.SHA256)}}
	injected := errors.New("stop after durable claim")
	manager := viewerinstall.Manager{Layout: layout, Registration: updateTestRegistration{}, FailAfter: func(phase viewerinstall.Phase) error {
		if phase == viewerinstall.PhasePreparing {
			return injected
		}
		return nil
	}}
	if err := manager.Apply(t.Context(), request); !errors.Is(err, injected) {
		t.Fatalf("Apply error=%v", err)
	}
	path := filepath.Join(layout.StateDir, "update.json")
	if err := SaveUpdateJournal(path, UpdateJournal{State: "installer_launched", TargetVersion: target.Version, ArtifactSHA256: digest, Generation: target.Generation, TransactionID: target.TransactionID}); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Config: Config{InstallDir: layout.InstallDir}, Paths: MachinePaths{Update: path}}
	manager.FailAfter = nil
	if err := withAvailableTransactionOwnership(layout, func(owner *viewerinstall.Ownership) error {
		if err := manager.ApplyOwned(t.Context(), owner, request); err != nil {
			return err
		}
		return agent.reconcileAbandonedInstallerHandoffOwned(layout)
	}); err != nil {
		t.Fatal(err)
	}
	transaction, err := viewerinstall.LoadJournal(layout)
	if err != nil || transaction.Phase != viewerinstall.PhaseCommitted {
		t.Fatalf("transaction=%+v err=%v", transaction, err)
	}
	journal, err := LoadUpdateJournal(path)
	if err != nil || journal.State != "installer_launched" {
		t.Fatalf("committed transaction was downgraded: journal=%+v err=%v", journal, err)
	}
}

func TestReconcileNeverResumesMismatchedIncompleteTransaction(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	transactionDigest := sha256Hex([]byte("transaction"))
	injected := errors.New("prepared unrelated transaction")
	manager := viewerinstall.Manager{Layout: layout, Registration: updateTestRegistration{}, FailAfter: func(phase viewerinstall.Phase) error {
		if phase == viewerinstall.PhasePreparing {
			return injected
		}
		return nil
	}}
	request := viewerinstall.Request{TransactionID: "other-transaction", Generation: 9, SourceDir: source, Release: viewerinstall.Release{Version: "9.0.0", Digest: transactionDigest, ReleaseID: viewerinstall.ReleaseID("9.0.0", transactionDigest)}}
	if err := manager.Apply(t.Context(), request); !errors.Is(err, injected) {
		t.Fatalf("Apply error=%v", err)
	}
	digest := sha256Hex([]byte("wanted"))
	paths := MachinePaths{State: filepath.Join(layout.StateDir, "state.json"), Commands: filepath.Join(layout.StateDir, "commands.json"), Update: filepath.Join(layout.StateDir, "update.json")}
	record := CommandRecord{ID: 88, Type: "update_app", PayloadHash: "p", OperationKey: "wanted-transaction", DesiredVersion: "2.1.6", ArtifactSHA256: digest, Generation: 10, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"88": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "installer_launched", TargetVersion: record.DesiredVersion, ArtifactSHA256: digest, Generation: record.Generation, TransactionID: record.OperationKey}); err != nil {
		t.Fatal(err)
	}
	called := 0
	agent := Agent{Config: Config{InstallDir: layout.InstallDir}, Paths: paths, Updater: updateExecutorFunc(func(context.Context, UpdateTarget) error {
		called++
		return nil
	})}
	results, err := agent.Reconcile(t.Context())
	if err != nil || len(results) != 0 || called != 0 {
		t.Fatalf("results=%+v calls=%d err=%v", results, called, err)
	}
	journal, err := LoadUpdateJournal(paths.Update)
	if err != nil || journal.State != "installer_launched" {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestReconcileRelaunchesExactLaunchingInstallerHandoff(t *testing.T) {
	dir := t.TempDir()
	digest := sha256Hex([]byte("setup"))
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	record := CommandRecord{ID: 84, Type: "update_app", PayloadHash: "p", OperationKey: "update-84", DesiredVersion: "2.1.2", ArtifactSHA256: digest, Generation: 6, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"84": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "launching_installer", TargetVersion: "2.1.2", ArtifactSHA256: digest, Generation: 6, TransactionID: "update-84"}); err != nil {
		t.Fatal(err)
	}
	called := 0
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(_ context.Context, target UpdateTarget) error {
		called++
		if target.TransactionID != "update-84" || target.Generation != 6 {
			t.Fatalf("target=%+v", target)
		}
		return ErrUpdateLaunched
	})}
	_, err := agent.Reconcile(t.Context())
	if !errors.Is(err, ErrAgentRestartRequested) || called != 1 {
		t.Fatalf("err=%v calls=%d", err, called)
	}
}

func TestReconcileResumesPersistedMetadataRetryWait(t *testing.T) {
	dir := t.TempDir()
	digest := sha256Hex([]byte("setup"))
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	record := CommandRecord{ID: 85, Type: "update_app", PayloadHash: "p", OperationKey: "update-85", DesiredVersion: "2.1.3", ArtifactSHA256: digest, Generation: 7, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"85": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "metadata_retry_wait", TargetVersion: "2.1.3", ArtifactSHA256: digest, Generation: 7, TransactionID: "update-85", MetadataAttempts: 1}); err != nil {
		t.Fatal(err)
	}
	called := 0
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(_ context.Context, target UpdateTarget) error {
		called++
		if target.TransactionID != "update-85" || target.Generation != 7 {
			t.Fatalf("target=%+v", target)
		}
		return ErrUpdateLaunched
	})}
	_, err := agent.Reconcile(t.Context())
	if !errors.Is(err, ErrAgentRestartRequested) || called != 1 {
		t.Fatalf("err=%v calls=%d", err, called)
	}
}

func TestReconcileTerminatesPersistedMetadataFailure(t *testing.T) {
	dir := t.TempDir()
	digest := sha256Hex([]byte("setup"))
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	record := CommandRecord{ID: 86, Type: "update_app", PayloadHash: "p", OperationKey: "update-86", DesiredVersion: "2.1.4", ArtifactSHA256: digest, Generation: 8, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"86": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "metadata_failed", LastError: "metadata_request_failed", TargetVersion: "2.1.4", ArtifactSHA256: digest, Generation: 8, TransactionID: "update-86", MetadataAttempts: 4}); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(context.Context, UpdateTarget) error {
		t.Fatal("terminal metadata failure was resumed")
		return nil
	})}
	results, err := agent.Reconcile(t.Context())
	if err != nil || len(results) != 1 || results[0].State != CommandFailed || results[0].Error != "metadata_request_failed" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	ledger, err := LoadCommandLedger(paths.Commands)
	if err != nil || ledger.Records["86"].State != CommandFailed {
		t.Fatalf("ledger=%+v err=%v", ledger, err)
	}
}

func TestReconcileTerminatesPersistedRejectedUpdate(t *testing.T) {
	dir := t.TempDir()
	digest := sha256Hex([]byte("setup"))
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	record := CommandRecord{ID: 87, Type: "update_app", PayloadHash: "p", OperationKey: "update-87", DesiredVersion: "2.1.5", ArtifactSHA256: digest, Generation: 9, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"87": record}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "rejected", LastError: "validation_failed", TargetVersion: "2.1.5", ArtifactSHA256: digest, Generation: 9, TransactionID: "update-87"}); err != nil {
		t.Fatal(err)
	}
	results, err := (&Agent{Paths: paths}).Reconcile(t.Context())
	if err != nil || len(results) != 1 || results[0].State != CommandRejected || results[0].Error != "validation_failed" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestCommittedTransactionReconcilesUpdateJournalAfterBoundaryFailure(t *testing.T) {
	stateDir, target := committedUpdateFixture(t)
	path := filepath.Join(stateDir, "update.json")
	journal := UpdateJournal{
		State: "installer_launched", TransactionID: target.TransactionID, Generation: target.Generation,
		CommandID: target.CommandID, PayloadHash: target.PayloadHash, TargetVersion: target.Version, ArtifactSHA256: target.SHA256,
	}
	if err := SaveUpdateJournal(path, journal); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("power loss after transaction commit")
	matched, err := reconcileCommittedUpdate(stateDir, func(string, UpdateJournal) error { return injected })
	if matched || !errors.Is(err, injected) {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	unchanged, err := LoadUpdateJournal(path)
	if err != nil || unchanged.State != "installer_launched" {
		t.Fatalf("update journal changed across failed boundary: %+v err=%v", unchanged, err)
	}
	matched, err = ReconcileCommittedUpdate(stateDir)
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	committed, err := LoadUpdateJournal(path)
	if err != nil || committed.State != "committed" {
		t.Fatalf("update journal=%+v err=%v", committed, err)
	}
}

func TestCommittedTransactionNeverReconcilesMismatchedUpdateJournal(t *testing.T) {
	stateDir, target := committedUpdateFixture(t)
	tests := []UpdateJournal{
		{State: "installer_launched", TransactionID: "other", CommandID: target.CommandID, PayloadHash: target.PayloadHash, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256},
		{State: "installer_launched", TransactionID: target.TransactionID, CommandID: target.CommandID + 1, PayloadHash: target.PayloadHash, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256},
		{State: "installer_launched", TransactionID: target.TransactionID, CommandID: target.CommandID, PayloadHash: "wrong", Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256},
		{State: "installer_launched", TransactionID: target.TransactionID, CommandID: target.CommandID, PayloadHash: target.PayloadHash, Generation: target.Generation + 1, TargetVersion: target.Version, ArtifactSHA256: target.SHA256},
		{State: "installer_launched", TransactionID: target.TransactionID, CommandID: target.CommandID, PayloadHash: target.PayloadHash, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: strings.Repeat("b", 64)},
	}
	for index, journal := range tests {
		if err := SaveUpdateJournal(filepath.Join(stateDir, "update.json"), journal); err != nil {
			t.Fatal(err)
		}
		matched, err := ReconcileCommittedUpdate(stateDir)
		if err != nil || matched {
			t.Fatalf("case %d matched=%v err=%v", index, matched, err)
		}
		unchanged, _ := LoadUpdateJournal(filepath.Join(stateDir, "update.json"))
		if unchanged.State == "committed" {
			t.Fatalf("case %d mismatched journal committed", index)
		}
	}
}

func TestRolledBackQuarantinedTransactionReconcilesRejectedUpdate(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := UpdateTarget{Version: "2.2.2", SHA256: strings.Repeat("c", 64), Generation: 13, TransactionID: "update-13"}
	request := viewerinstall.Request{TransactionID: target.TransactionID, Generation: target.Generation, SourceDir: source, Release: viewerinstall.Release{Version: target.Version, Digest: target.SHA256, ReleaseID: viewerinstall.ReleaseID(target.Version, target.SHA256)}}
	manager := viewerinstall.Manager{Layout: layout, Registration: updateFailingRegistration{}}
	if err := manager.Apply(t.Context(), request); err == nil {
		t.Fatal("failed update transaction committed")
	}
	path := filepath.Join(layout.StateDir, "update.json")
	if err := SaveUpdateJournal(path, UpdateJournal{State: "installer_launched", TransactionID: target.TransactionID, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256}); err != nil {
		t.Fatal(err)
	}
	matched, err := ReconcileCommittedUpdate(layout.StateDir)
	journal, loadErr := LoadUpdateJournal(path)
	if err != nil || loadErr != nil || matched || journal.State != "rejected" || !journal.IsQuarantined(target.Version, target.SHA256, target.Generation) {
		t.Fatalf("matched=%v journal=%+v err=%v loadErr=%v", matched, journal, err, loadErr)
	}
}

func TestAgentReconcileFinishesCommandFromMatchingCommittedTransaction(t *testing.T) {
	stateDir, target := committedUpdateFixture(t)
	paths := MachinePaths{State: filepath.Join(stateDir, "state.json"), Commands: filepath.Join(stateDir, "commands.json"), Update: filepath.Join(stateDir, "update.json")}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "installer_launched", TransactionID: target.TransactionID, CommandID: target.CommandID, PayloadHash: target.PayloadHash, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256}); err != nil {
		t.Fatal(err)
	}
	record := CommandRecord{ID: target.CommandID, Type: "update_app", PayloadHash: target.PayloadHash, OperationKey: target.TransactionID, DesiredVersion: target.Version, ArtifactSHA256: target.SHA256, Generation: target.Generation, State: CommandRunning}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"91": record}}); err != nil {
		t.Fatal(err)
	}
	results, err := (&Agent{Paths: paths}).Reconcile(t.Context())
	if err != nil || len(results) != 1 || results[0].State != CommandSucceeded {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func committedUpdateFixture(t *testing.T) (string, UpdateTarget) {
	t.Helper()
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := UpdateTarget{Version: "2.2.0", SHA256: strings.Repeat("a", 64), Generation: 11, TransactionID: "update-11", CommandID: 91, PayloadHash: strings.Repeat("d", 64)}
	request := viewerinstall.Request{TransactionID: target.TransactionID, CommandID: target.CommandID, PayloadHash: target.PayloadHash, Generation: target.Generation, SourceDir: source, Release: viewerinstall.Release{Version: target.Version, Digest: target.SHA256, ReleaseID: viewerinstall.ReleaseID(target.Version, target.SHA256)}}
	if err := (viewerinstall.Manager{Layout: layout, Registration: updateTestRegistration{}}).Apply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	return layout.StateDir, target
}

type updateTestRegistration struct{}

func (updateTestRegistration) Stop(context.Context) error                            { return nil }
func (updateTestRegistration) Start(context.Context) error                           { return nil }
func (updateTestRegistration) Validate(context.Context, viewerinstall.Journal) error { return nil }

type updateFailingRegistration struct{ updateTestRegistration }

func (updateFailingRegistration) Validate(context.Context, viewerinstall.Journal) error {
	return errors.New("new release unhealthy")
}

func TestViewerReadyGateRequiresContinuousReadyWindow(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json")}
	if err := SaveMachineState(paths.State, MachineState{ViewerState: "running", RendererState: "not_ready"}); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths}
	done := make(chan error, 1)
	go func() { done <- agent.waitViewerReady(t.Context(), 20*time.Millisecond) }()
	time.Sleep(10 * time.Millisecond)
	now := time.Now()
	if err := SaveMachineState(paths.State, MachineState{
		ViewerGeneration: 1, ExpectedViewerGeneration: 1,
		ViewerState: "running", RendererState: "ready", ViewerLastHeartbeatAt: &now, RendererLastHeartbeatAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Viewer-ready gate did not open")
	}
}

func TestViewerReadyGateDoesNotTrustStaleGreenState(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json")}
	stale := time.Now().Add(-time.Minute)
	if err := SaveMachineState(paths.State, MachineState{ViewerState: "running", RendererState: "ready", ViewerLastHeartbeatAt: &stale}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := (&Agent{Paths: paths}).waitViewerReady(ctx, 5*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale Viewer opened update gate: %v", err)
	}
}

func TestUpdateActivationRejectsReadinessGenerationRace(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Update: filepath.Join(dir, "update.json")}
	now := time.Now().UTC()
	if err := SaveMachineState(paths.State, MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 7,
		ViewerState: "running", RendererState: "ready",
		ViewerLastHeartbeatAt: &now, RendererLastHeartbeatAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	launches := 0
	prepared := preparedUpdate{
		journalPath:   paths.Update,
		journal:       UpdateJournal{State: "verified", TargetVersion: "2.9.2", ArtifactSHA256: strings.Repeat("b", 64), Generation: 4, TransactionID: "update-race"},
		installerPath: filepath.Join(dir, "CamStationViewerSetup.exe"),
		args:          []string{"--update"},
		launch: func(string, []string) error {
			launches++
			return nil
		},
	}
	if err := prepared.markWaiting(); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths}
	readyGeneration, err := agent.waitViewerReadyGeneration(t.Context(), 10*time.Millisecond)
	if err != nil || readyGeneration != 7 {
		t.Fatalf("ready generation=%d err=%v", readyGeneration, err)
	}

	racedAt := time.Now().UTC()
	if err := SaveMachineState(paths.State, MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 8,
		ViewerState: "restart_authorized", RendererState: "not_ready",
		ViewerLastHeartbeatAt: &racedAt, RendererLastHeartbeatAt: &racedAt,
	}); err != nil {
		t.Fatal(err)
	}
	launched, err := agent.launchPreparedUpdateIfReady(t.Context(), prepared, readyGeneration)
	if err != nil || launched || launches != 0 {
		t.Fatalf("raced activation launched=%v calls=%d err=%v", launched, launches, err)
	}

	readyAt := time.Now().UTC()
	if err := SaveMachineState(paths.State, MachineState{
		ViewerGeneration: 8, ExpectedViewerGeneration: 8,
		ViewerState: "running", RendererState: "ready",
		ViewerLastHeartbeatAt: &readyAt, RendererLastHeartbeatAt: &readyAt,
	}); err != nil {
		t.Fatal(err)
	}
	readyGeneration, err = agent.waitViewerReadyGeneration(t.Context(), 10*time.Millisecond)
	if err != nil || readyGeneration != 8 {
		t.Fatalf("replacement generation=%d err=%v", readyGeneration, err)
	}
	launched, err = agent.launchPreparedUpdateIfReady(t.Context(), prepared, readyGeneration)
	if !errors.Is(err, ErrUpdateLaunched) || !launched || launches != 1 {
		t.Fatalf("exact activation launched=%v calls=%d err=%v", launched, launches, err)
	}
}

func releaseMetadata(version string, size int64, digest string, unsigned bool) ReleaseMetadata {
	return ReleaseMetadata{Version: version, Filename: "CamStationViewerSetup.exe", SizeBytes: size, SHA256: digest, DevelopmentUnsigned: unsigned, DownloadURL: "/api/viewers/app/download"}
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
