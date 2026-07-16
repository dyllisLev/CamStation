package vieweragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	target := UpdateTarget{Version: "2.0.1", SHA256: digest, Generation: 9, TransactionID: "update-9"}
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
	wantArgs := []string{"--update", "--transaction-id", "update-9", "--generation", "9", "--expected-sha", digest, "--parent-pid"}
	if len(args) != len(wantArgs)+1 || !reflect.DeepEqual(args[:len(wantArgs)], wantArgs) {
		t.Fatalf("launch args=%v", args)
	}
	journal, err := LoadUpdateJournal(filepath.Join(runner.StateDir, "update.json"))
	if err != nil || journal.State != "installer_launched" || journal.DownloadAttempts != 1 || journal.TransactionID != target.TransactionID {
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
	started := time.Now()
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.9", SHA256: digest, Generation: 9, TransactionID: "tx-9"})
	if err == nil || time.Since(started) > time.Second || requests.Load() != 4 {
		t.Fatalf("err=%v elapsed=%v requests=%d", err, time.Since(started), requests.Load())
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
	started := time.Now()
	err := runner.Run(t.Context(), UpdateTarget{Version: "2.0.10", SHA256: digest, Generation: 10, TransactionID: "tx-10"})
	if err == nil || time.Since(started) > time.Second || downloads.Load() != 4 {
		t.Fatalf("err=%v elapsed=%v downloads=%d", err, time.Since(started), downloads.Load())
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
	if received.Version != command.DesiredVersion || received.SHA256 != digest || received.Generation != 12 || received.TransactionID != record.OperationKey {
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

func TestReconcileDoesNotRelaunchLiveInstallerTransaction(t *testing.T) {
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
	agent := Agent{Paths: paths, Updater: updateExecutorFunc(func(context.Context, UpdateTarget) error {
		t.Fatal("live installer transaction was relaunched")
		return nil
	})}
	results, err := agent.Reconcile(t.Context())
	if err != nil || len(results) != 0 {
		t.Fatalf("results=%+v err=%v", results, err)
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

func TestCommittedTransactionReconcilesUpdateJournalAfterBoundaryFailure(t *testing.T) {
	stateDir, target := committedUpdateFixture(t)
	path := filepath.Join(stateDir, "update.json")
	journal := UpdateJournal{
		State: "installer_launched", TransactionID: target.TransactionID, Generation: target.Generation,
		TargetVersion: target.Version, ArtifactSHA256: target.SHA256,
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
		{State: "installer_launched", TransactionID: "other", Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256},
		{State: "installer_launched", TransactionID: target.TransactionID, Generation: target.Generation + 1, TargetVersion: target.Version, ArtifactSHA256: target.SHA256},
		{State: "installer_launched", TransactionID: target.TransactionID, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: strings.Repeat("b", 64)},
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

func TestAgentReconcileFinishesCommandFromMatchingCommittedTransaction(t *testing.T) {
	stateDir, target := committedUpdateFixture(t)
	paths := MachinePaths{State: filepath.Join(stateDir, "state.json"), Commands: filepath.Join(stateDir, "commands.json"), Update: filepath.Join(stateDir, "update.json")}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{State: "installer_launched", TransactionID: target.TransactionID, Generation: target.Generation, TargetVersion: target.Version, ArtifactSHA256: target.SHA256}); err != nil {
		t.Fatal(err)
	}
	record := CommandRecord{ID: 91, Type: "update_app", PayloadHash: "p", OperationKey: target.TransactionID, DesiredVersion: target.Version, ArtifactSHA256: target.SHA256, Generation: target.Generation, State: CommandRunning}
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
	target := UpdateTarget{Version: "2.2.0", SHA256: strings.Repeat("a", 64), Generation: 11, TransactionID: "update-11"}
	request := viewerinstall.Request{TransactionID: target.TransactionID, Generation: target.Generation, SourceDir: source, Release: viewerinstall.Release{Version: target.Version, Digest: target.SHA256, ReleaseID: viewerinstall.ReleaseID(target.Version, target.SHA256)}}
	if err := (viewerinstall.Manager{Layout: layout, Registration: updateTestRegistration{}}).Apply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	return layout.StateDir, target
}

type updateTestRegistration struct{}

func (updateTestRegistration) Stop(context.Context) error                            { return nil }
func (updateTestRegistration) Start(context.Context) error                           { return nil }
func (updateTestRegistration) Validate(context.Context, viewerinstall.Journal) error { return nil }

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
	if err := SaveMachineState(paths.State, MachineState{ViewerState: "running", RendererState: "ready", ViewerLastHeartbeatAt: &now}); err != nil {
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

func releaseMetadata(version string, size int64, digest string, unsigned bool) ReleaseMetadata {
	return ReleaseMetadata{Version: version, Filename: "CamStationViewerSetup.exe", SizeBytes: size, SHA256: digest, DevelopmentUnsigned: unsigned, DownloadURL: "/api/viewers/app/download"}
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
