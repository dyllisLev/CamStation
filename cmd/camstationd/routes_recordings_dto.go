package main

import (
	"strconv"

	"camstation/internal/store"
)

type recordingSegmentResponse struct {
	ID          int64    `json:"id"`
	CameraID    int64    `json:"camera_id"`
	StreamName  string   `json:"streamName"`
	Filename    string   `json:"filename"`
	TSStart     float64  `json:"ts_start"`
	TSEnd       *float64 `json:"ts_end"`
	FileSize    *int64   `json:"file_size"`
	Status      string   `json:"status"`
	BackupState string   `json:"backupState"`
	BackedUpAt  string   `json:"backedUpAt,omitempty"`
	BackupJobID int64    `json:"backupJobId,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	PlayURL     string   `json:"playUrl,omitempty"`
	DownloadURL string   `json:"downloadUrl,omitempty"`
}

func recordingSegmentResponses(segments []store.RecordingSegment) []recordingSegmentResponse {
	out := make([]recordingSegmentResponse, 0, len(segments))
	for _, segment := range segments {
		out = append(out, recordingSegmentResponseFromStore(segment))
	}
	return out
}

func recordingSegmentResponseFromStore(segment store.RecordingSegment) recordingSegmentResponse {
	response := recordingSegmentResponse{
		ID:          segment.ID,
		CameraID:    segment.CameraID,
		StreamName:  segment.StreamName,
		Filename:    segment.Filename,
		TSStart:     segment.TSStart,
		TSEnd:       segment.TSEnd,
		FileSize:    segment.FileSize,
		Status:      segment.Status,
		BackupState: segment.BackupState,
		BackedUpAt:  segment.BackedUpAt,
		BackupJobID: segment.BackupJobID,
		CreatedAt:   segment.CreatedAt,
		UpdatedAt:   segment.UpdatedAt,
	}
	if segment.Status == "ready" {
		base := "/api/recordings/segments/" + strconv.FormatInt(segment.ID, 10)
		response.PlayURL = base + "/play"
		response.DownloadURL = base + "/download"
	}
	return response
}
