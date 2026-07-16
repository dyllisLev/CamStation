package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"camstation/internal/vieweragent"
	"camstation/internal/viewerinstall"
)

//go:embed all:payload
var payloadFS embed.FS

type installerMode string

const (
	modeInstall   installerMode = "install"
	modeUpdate    installerMode = "update"
	modeRollback  installerMode = "rollback"
	modeUninstall installerMode = "uninstall"
	modeRecover   installerMode = "recover"
)

var errCommitJournalBoundary = errors.New("transaction committed before update journal")

type installerOptions struct {
	mode          installerMode
	silent        bool
	transactionID string
	generation    int64
	expectedSHA   string
	parentPID     int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "CamStation Viewer Setup:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options, err := parseInstallerArgs(args)
	if err != nil {
		return err
	}
	relaunched, err := ensureElevated(args)
	if err != nil || relaunched {
		return err
	}
	progress := installerProgress(options.silent, os.Stdout)
	progress("Preparing installation")
	layout := viewerinstall.DefaultLayout()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	switch options.mode {
	case modeUninstall:
		progress("Stopping and removing CamStation Viewer")
		if err := viewerinstall.UnregisterAll(ctx, layout); err != nil {
			return err
		}
		err = removeInstallation(layout)
	case modeRecover:
		progress("Recovering the last installation transaction")
		err = recoverAndReconcile(ctx, layout, viewerinstall.SystemRegistration{Layout: layout})
	case modeRollback:
		progress("Rolling back CamStation Viewer")
		err = (viewerinstall.Manager{Layout: layout, Registration: viewerinstall.SystemRegistration{Layout: layout}}).Rollback(ctx, options.transactionID)
	case modeUpdate:
		progress("Waiting for the running agent to stop")
		if err := waitParent(options.parentPID, 30*time.Second); err != nil {
			return err
		}
		err = installPayload(ctx, layout, options, progress)
	default:
		err = installPayload(ctx, layout, options, progress)
	}
	if err == nil {
		progress("Installation complete")
	}
	return err
}

func installerProgress(silent bool, output io.Writer) func(string) {
	if silent || output == nil {
		return func(string) {}
	}
	return func(message string) {
		_, _ = fmt.Fprintln(output, "CamStation Viewer Setup:", message)
	}
}

func parseInstallerArgs(args []string) (installerOptions, error) {
	options := installerOptions{mode: modeInstall}
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.EqualFold(arg, "/S") {
			options.silent = true
			continue
		}
		filtered = append(filtered, arg)
	}
	flags := flag.NewFlagSet("CamStationViewerSetup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	update := flags.Bool("update", false, "apply embedded update")
	rollback := flags.String("rollback", "", "rollback transaction")
	uninstall := flags.Bool("uninstall", false, "uninstall")
	recoverUpdate := flags.Bool("recover", false, "recover interrupted update")
	transactionID := flags.String("transaction-id", "", "update transaction")
	generation := flags.Int64("generation", 0, "command generation")
	expectedSHA := flags.String("expected-sha", "", "verified installer SHA-256")
	parentPID := flags.Int("parent-pid", 0, "Agent process to drain")
	if err := flags.Parse(filtered); err != nil || flags.NArg() != 0 {
		return installerOptions{}, errors.New("invalid installer arguments")
	}
	modes := 0
	if *update {
		options.mode = modeUpdate
		modes++
	}
	if *rollback != "" {
		options.mode = modeRollback
		modes++
	}
	if *uninstall {
		options.mode = modeUninstall
		modes++
	}
	if *recoverUpdate {
		options.mode = modeRecover
		modes++
	}
	if modes > 1 {
		return installerOptions{}, errors.New("installer modes are mutually exclusive")
	}
	options.transactionID = *transactionID
	options.generation = *generation
	options.expectedSHA = strings.ToLower(*expectedSHA)
	options.parentPID = *parentPID
	switch options.mode {
	case modeUpdate:
		if !safeIdentifier(options.transactionID, 128) || options.generation <= 0 || !validSHA(options.expectedSHA) || options.parentPID < 0 {
			return installerOptions{}, errors.New("complete verified update arguments are required")
		}
	case modeRollback:
		if !safeIdentifier(*rollback, 128) || options.transactionID != "" || options.generation != 0 || options.expectedSHA != "" || options.parentPID != 0 {
			return installerOptions{}, errors.New("invalid rollback arguments")
		}
		options.transactionID = *rollback
	case modeInstall, modeUninstall, modeRecover:
		if options.transactionID != "" || options.generation != 0 || options.expectedSHA != "" || options.parentPID != 0 {
			return installerOptions{}, errors.New("unexpected installer transaction arguments")
		}
	}
	return options, nil
}

func installPayload(ctx context.Context, layout viewerinstall.Layout, options installerOptions, progress func(string)) error {
	if options.mode == modeUpdate {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		if err := verifyFileSHA(executable, options.expectedSHA); err != nil {
			return err
		}
	}
	progress("Verifying the embedded release")
	payload, err := payloadFS.ReadFile("payload/release.zip")
	if err != nil {
		return errors.New("embedded Viewer release payload is missing")
	}
	temp, err := os.MkdirTemp("", "camstation-viewer-payload-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	manifest, err := viewerinstall.ExtractPayload(bytes.NewReader(payload), int64(len(payload)), temp)
	if err != nil {
		return err
	}
	defaults, err := loadDefaults(filepath.Join(temp, "defaults.json"))
	if err != nil {
		return err
	}
	if options.mode == modeUpdate {
		progress("Applying the verified update")
		if err := installUpdate(ctx, layout, options, manifest, temp); err != nil {
			if errors.Is(err, viewerinstall.ErrUpdateOwned) {
				return nil
			}
			if !errors.Is(err, errCommitJournalBoundary) {
				markUpdateFailed(layout, options, err)
			}
			return err
		}
		matched, err := vieweragent.ReconcileCommittedUpdate(layout.StateDir)
		if err != nil {
			return errors.Join(errCommitJournalBoundary, err)
		}
		if !matched {
			return errors.Join(errCommitJournalBoundary, errors.New("committed transaction did not match update handoff"))
		}
		return nil
	}
	progress("Installing CamStation Viewer services")
	return installInitial(ctx, layout, defaults, manifest, temp)
}

func installInitial(ctx context.Context, layout viewerinstall.Layout, defaults viewerinstall.PayloadDefaults, manifest viewerinstall.PayloadManifest, payloadDir string) error {
	if _, err := vieweragent.ValidateServerURL(defaults.ServerURL); err != nil {
		return err
	}
	monitoringSID, err := viewerinstall.ActiveConsoleUserSID(ctx)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	displayName := strings.TrimSpace(defaults.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(hostname) + " Viewer"
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	_ = (viewerinstall.SystemRegistration{Layout: layout}).Stop(ctx)
	if err := viewerinstall.InstallStablePayload(layout, payloadDir, executable); err != nil {
		return err
	}
	serviceSID, err := viewerinstall.RegisterAll(ctx, layout, viewerinstall.RegistrationOptions{MonitoringUserSID: monitoringSID, ServerURL: defaults.ServerURL, DisplayName: displayName})
	if err != nil {
		return err
	}
	if _, err := vieweragent.ConfigureInstaller(filepath.Join(layout.StateDir, "config.json"), vieweragent.InstallerConfig{
		ServerURL: defaults.ServerURL, DisplayName: displayName, InstallDir: layout.InstallDir,
		MonitoringUserSID: monitoringSID, AgentServiceSID: serviceSID,
		AllowDevelopmentUnsigned: defaults.AllowDevelopmentUnsigned, SignerThumbprint: defaults.SignerThumbprint,
	}); err != nil {
		return err
	}
	release := viewerinstall.Release{Version: manifest.Version, Digest: manifest.Digest, ReleaseID: viewerinstall.ReleaseID(manifest.Version, manifest.Digest)}
	request := viewerinstall.Request{TransactionID: "install-" + manifest.Digest[:16], Generation: 1, SourceDir: filepath.Join(payloadDir, "release"), Release: release}
	return (viewerinstall.Manager{Layout: layout, Registration: viewerinstall.SystemRegistration{Layout: layout}}).Apply(ctx, request)
}

func installUpdate(ctx context.Context, layout viewerinstall.Layout, options installerOptions, manifest viewerinstall.PayloadManifest, payloadDir string) error {
	if err := promoteUpdateHandoff(layout, options, manifest.Version); err != nil {
		return err
	}
	config, err := vieweragent.LoadConfig(filepath.Join(layout.StateDir, "config.json"))
	if err != nil {
		return err
	}
	if config.InstallDir != layout.InstallDir {
		return errors.New("installed Viewer layout does not match updater")
	}
	release := viewerinstall.Release{Version: manifest.Version, Digest: options.expectedSHA, ReleaseID: viewerinstall.ReleaseID(manifest.Version, options.expectedSHA)}
	request := viewerinstall.Request{TransactionID: options.transactionID, Generation: options.generation, SourceDir: filepath.Join(payloadDir, "release"), Release: release}
	return (viewerinstall.Manager{Layout: layout, Registration: viewerinstall.SystemRegistration{Layout: layout}}).Apply(ctx, request)
}

func promoteUpdateHandoff(layout viewerinstall.Layout, options installerOptions, version string) error {
	owner, err := viewerinstall.Acquire(layout)
	if err != nil {
		return err
	}
	defer owner.Close()
	path := filepath.Join(layout.StateDir, "update.json")
	journal, err := vieweragent.LoadUpdateJournal(path)
	if err != nil {
		return err
	}
	if err := validateUpdateHandoff(journal, options, version); err != nil {
		return err
	}
	if journal.State == "installer_launched" {
		return nil
	}
	journal.State = "installer_launched"
	return vieweragent.SaveUpdateJournal(path, journal)
}

func verifyFileSHA(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("updater executable is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return errors.New("updater executable SHA-256 does not match Agent handoff")
	}
	return nil
}

func validateUpdateHandoff(journal vieweragent.UpdateJournal, options installerOptions, version string) error {
	if (journal.State != "launching_installer" && journal.State != "installer_launched") || journal.TransactionID != options.transactionID || journal.Generation != options.generation ||
		journal.ArtifactSHA256 != options.expectedSHA || journal.TargetVersion != version {
		return errors.New("update ledger does not match Agent handoff")
	}
	return nil
}

func loadDefaults(path string) (viewerinstall.PayloadDefaults, error) {
	file, err := os.Open(path)
	if err != nil {
		return viewerinstall.PayloadDefaults{}, err
	}
	defer file.Close()
	var defaults viewerinstall.PayloadDefaults
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&defaults); err != nil {
		return viewerinstall.PayloadDefaults{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return viewerinstall.PayloadDefaults{}, errors.New("defaults contain trailing data")
	}
	return defaults, nil
}

func markUpdateFailed(layout viewerinstall.Layout, options installerOptions, cause error) {
	path := filepath.Join(layout.StateDir, "update.json")
	journal, err := vieweragent.LoadUpdateJournal(path)
	if err != nil {
		return
	}
	journal.State = "rolled_back"
	journal.LastError = "installation_failed"
	journal.Quarantine(journal.TargetVersion, options.expectedSHA, options.generation, time.Now().UTC(), "installation_failed")
	_ = vieweragent.SaveUpdateJournal(path, journal)
	_ = cause
}

func recoverAndReconcile(ctx context.Context, layout viewerinstall.Layout, registration viewerinstall.Registration) error {
	if err := (viewerinstall.Manager{Layout: layout, Registration: registration}).Recover(ctx); err != nil {
		return err
	}
	_, err := vieweragent.ReconcileCommittedUpdate(layout.StateDir)
	return err
}

func safeIdentifier(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._-", char)) {
			return false
		}
	}
	return true
}

func validSHA(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
