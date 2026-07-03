package store

import "time"

type JobState string

const (
	JobStateQueued    JobState = "queued"
	JobStateRunning   JobState = "running"
	JobStateSucceeded JobState = "succeeded"
	JobStateFailed    JobState = "failed"
	JobStateCancelled JobState = "cancelled"
	JobStateDeleted   JobState = "deleted"
)

type JobCreate struct {
	Kind            string `json:"kind"`
	SingleFlightKey string `json:"singleFlightKey,omitempty"`
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
}

type Job struct {
	ID              int64          `json:"id"`
	Kind            string         `json:"kind"`
	SingleFlightKey string         `json:"singleFlightKey,omitempty"`
	State           JobState       `json:"state"`
	TimeoutSeconds  int            `json:"timeoutSeconds,omitempty"`
	Error           string         `json:"error,omitempty"`
	Result          map[string]any `json:"result,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	StartedAt       *time.Time     `json:"startedAt,omitempty"`
	CompletedAt     *time.Time     `json:"completedAt,omitempty"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	Events          []JobEvent     `json:"events,omitempty"`
}

type JobEvent struct {
	ID        int64          `json:"id"`
	JobID     int64          `json:"jobId"`
	CreatedAt time.Time      `json:"createdAt"`
	Type      string         `json:"type"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}
