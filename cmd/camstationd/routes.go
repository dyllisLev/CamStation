package main

import (
	"context"
	"io/fs"
	"net/http"

	"camstation/internal/backup"
	"camstation/internal/camera"
	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type streamController interface {
	Status(context.Context) stream.Status
	Restart(context.Context, []store.Camera) error
}

type routeDeps struct {
	db               *store.DB
	prober           camera.Prober
	streamer         streamController
	recorderManager  *recorder.Manager
	cleaner          *cleanup.Cleaner
	backupRunner     *backup.Runner
	recordingsDir    string
	tempDir          string
	maxStorageBytes  int64
	recordingEnabled bool
}

func routes(db *store.DB, prober camera.Prober, streamer *stream.Go2RTC, recorderManager *recorder.Manager, cleaner *cleanup.Cleaner, recordingsDir, tempDir string, maxStorageBytes int64, recordingEnabled bool, backupRunnerOpt ...*backup.Runner) (http.Handler, error) {
	var backupRunner *backup.Runner
	if len(backupRunnerOpt) > 0 {
		backupRunner = backupRunnerOpt[0]
	}
	if backupRunner == nil {
		backupRunner = buildBackupRunner(db)
	}
	return routeDeps{
		db:               db,
		prober:           prober,
		streamer:         streamer,
		recorderManager:  recorderManager,
		cleaner:          cleaner,
		backupRunner:     backupRunner,
		recordingsDir:    recordingsDir,
		tempDir:          tempDir,
		maxStorageBytes:  maxStorageBytes,
		recordingEnabled: recordingEnabled,
	}.handler()
}

func (d routeDeps) handler() (http.Handler, error) {
	mux := http.NewServeMux()
	previews := newPreviewRegistry()

	d.registerCoreRoutes(mux)
	d.registerCameraRoutes(mux, previews)
	d.registerStreamRoutes(mux)
	d.registerViewerRoutes(mux)
	d.registerSystemRoutes(mux)
	d.registerSettingsJobRoutes(mux)
	d.registerAlertRoutes(mux)
	d.registerRecordingRoutes(mux)
	d.registerBackupRoutes(mux)
	d.registerEventIncidentRoutes(mux)

	liveProxy, err := go2RTCProxy(previews)
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
