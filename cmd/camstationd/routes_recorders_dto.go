package main

import "camstation/internal/recorder"

const (
	publicManagedRecordingsDir = "managed-recordings"
	publicManagedTempDir       = "managed-temp"
	publicInternalStreamInput  = "internal-stream"
	publicManagedSegment       = "managed-segment"
	publicRecorderWorkerError  = "녹화 오류가 발생했습니다"
)

type publicRecorderStatus struct {
	Enabled        bool                         `json:"enabled"`
	RecordingsDir  string                       `json:"recordingsDir"`
	TempDir        string                       `json:"tempDir"`
	SegmentMinutes int                          `json:"segmentMinutes"`
	Workers        []publicRecorderWorkerStatus `json:"workers"`
}

type publicRecorderWorkerStatus struct {
	StreamName string `json:"streamName"`
	CameraID   int64  `json:"camera_id"`
	State      string `json:"state"`
	Input      string `json:"input"`
	Current    string `json:"current,omitempty"`
	LastError  string `json:"lastError,omitempty"`
}

type publicRecordingStorage struct {
	RecordingsDir      string `json:"recordingsDir"`
	TempDir            string `json:"tempDir"`
	RecordingsBytes    int64  `json:"recordingsBytes"`
	TempBytes          int64  `json:"tempBytes"`
	MaxBytes           int64  `json:"maxBytes"`
	AutoCleanupEnabled bool   `json:"autoCleanupEnabled"`
}

func publicRecorderStatusFromInternal(status recorder.Status) publicRecorderStatus {
	workers := make([]publicRecorderWorkerStatus, 0, len(status.Workers))
	for _, worker := range status.Workers {
		workers = append(workers, publicRecorderWorkerStatusFromInternal(worker))
	}
	return publicRecorderStatus{
		Enabled:        status.Enabled,
		RecordingsDir:  publicManagedRecordingsDir,
		TempDir:        publicManagedTempDir,
		SegmentMinutes: status.SegmentMinutes,
		Workers:        workers,
	}
}

func publicRecorderWorkerStatusFromInternal(worker recorder.WorkerStatus) publicRecorderWorkerStatus {
	current := ""
	if worker.Current != "" {
		current = publicManagedSegment
	}
	lastError := ""
	if worker.LastError != "" {
		lastError = publicRecorderWorkerError
	}
	return publicRecorderWorkerStatus{
		StreamName: worker.StreamName,
		CameraID:   worker.CameraID,
		State:      worker.State,
		Input:      publicInternalStreamInput,
		Current:    current,
		LastError:  lastError,
	}
}
