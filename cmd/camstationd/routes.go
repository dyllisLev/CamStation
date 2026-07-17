package main

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"camstation/internal/backup"
	"camstation/internal/camera"
	"camstation/internal/cameracontrol"
	"camstation/internal/cleanup"
	"camstation/internal/onvif"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
	"camstation/internal/viewerrelease"
)

type streamController interface {
	Status(context.Context) stream.Status
	Restart(context.Context, []store.Camera) error
}

type policyApplier interface {
	Apply(context.Context) stream.PolicyApplyResult
	ApplyExpected(context.Context, int64, int64) stream.PolicyApplyResult
}

type cameraControlService interface {
	Discover(context.Context, store.Camera) (store.CameraControlCapabilities, error)
	Status(context.Context, store.Camera) (cameracontrol.Status, error)
	Move(context.Context, store.Camera, cameracontrol.MoveVector) error
	Stop(context.Context, store.Camera) error
	GotoHome(context.Context, store.Camera) error
	SetHome(context.Context, store.Camera) error
	ListPresets(context.Context, store.Camera) ([]cameracontrol.Preset, error)
	CreatePreset(context.Context, store.Camera, string) (cameracontrol.Preset, error)
	GotoPreset(context.Context, store.Camera, string) error
	DeletePreset(context.Context, store.Camera, string) error
}

type routeDeps struct {
	db                *store.DB
	prober            camera.Prober
	streamer          streamController
	policyApplier     policyApplier
	recorderManager   *recorder.Manager
	cleaner           *cleanup.Cleaner
	backupRunner      *backup.Runner
	recordingsDir     string
	tempDir           string
	viewerReleasesDir string
	viewerReleases    *viewerrelease.Catalog
	maxStorageBytes   int64
	recordingEnabled  bool
	cameraController  cameraControlService
	presetLocks       *cameraPresetLockSet
	activationMu      *sync.Mutex
}

func routes(db *store.DB, prober camera.Prober, streamer *stream.Go2RTC, recorderManager *recorder.Manager, cleaner *cleanup.Cleaner, recordingsDir, tempDir string, maxStorageBytes int64, recordingEnabled bool, backupRunnerOpt ...*backup.Runner) (http.Handler, error) {
	var backupRunner *backup.Runner
	if len(backupRunnerOpt) > 0 {
		backupRunner = backupRunnerOpt[0]
	}
	if backupRunner == nil {
		backupRunner = buildBackupRunner(db)
	}
	return routesWithPolicyApplier(db, prober, streamer, recorderManager, cleaner, recordingsDir, tempDir, maxStorageBytes, recordingEnabled, backupRunner, stream.NewApplyCoordinator(db, streamer, recorderManager))
}

func routesWithPolicyApplier(db *store.DB, prober camera.Prober, streamer *stream.Go2RTC, recorderManager *recorder.Manager, cleaner *cleanup.Cleaner, recordingsDir, tempDir string, maxStorageBytes int64, recordingEnabled bool, backupRunner *backup.Runner, applier policyApplier, viewerReleasesDirOpt ...string) (http.Handler, error) {
	deps := routeDeps{
		db:               db,
		prober:           prober,
		streamer:         streamer,
		policyApplier:    applier,
		recorderManager:  recorderManager,
		cleaner:          cleaner,
		backupRunner:     backupRunner,
		recordingsDir:    recordingsDir,
		tempDir:          tempDir,
		maxStorageBytes:  maxStorageBytes,
		recordingEnabled: recordingEnabled,
	}
	if len(viewerReleasesDirOpt) > 0 {
		deps.viewerReleasesDir = viewerReleasesDirOpt[0]
	}
	return deps.handler()
}

func (d routeDeps) handler() (http.Handler, error) {
	if d.cameraController == nil {
		d.cameraController = cameracontrol.New(onvif.NewClient(&http.Client{Timeout: 8 * time.Second}))
	}
	if d.presetLocks == nil {
		d.presetLocks = &cameraPresetLockSet{}
	}
	if d.activationMu == nil {
		d.activationMu = &sync.Mutex{}
	}
	if d.viewerReleasesDir == "" {
		d.viewerReleasesDir = filepath.Join(filepath.Dir(d.recordingsDir), "viewer-releases")
	}
	if d.viewerReleases == nil {
		d.viewerReleases = viewerrelease.NewCatalog(d.viewerReleasesDir)
	}
	mux := http.NewServeMux()
	previews := newPreviewRegistry()

	d.registerCoreRoutes(mux)
	d.registerCameraRoutes(mux, previews)
	d.registerStreamRoutes(mux)
	d.registerViewerRoutes(mux)
	d.registerViewerReleaseRoutes(mux)
	d.registerSystemRoutes(mux)
	d.registerSettingsJobRoutes(mux)
	d.registerAlertRoutes(mux)
	d.registerRecordingRoutes(mux)
	d.registerBackupRoutes(mux)
	d.registerEventIncidentRoutes(mux)

	liveProxy, err := go2RTCProxy(previews, func(ctx context.Context, streamName string) bool {
		cameras, err := d.db.ListCameras(ctx, false)
		return err == nil && isRegisteredPublicStream(cameras, streamName)
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/player/", http.StripPrefix("/player", liveProxy))

	d.registerProbeRoute(mux)

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	mux.Handle("/", spaHandler(http.FS(static)))
	return mux, nil
}
