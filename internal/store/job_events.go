package store

import (
	"context"
	"fmt"
	"strings"
)

type JobEventAppend struct {
	JobID   int64
	Type    string
	Message string
	Details map[string]any
}

func (d *DB) AppendJobEvent(ctx context.Context, event JobEventAppend) error {
	event.Type = strings.TrimSpace(event.Type)
	event.Message = strings.TrimSpace(event.Message)
	if event.JobID <= 0 {
		return fmt.Errorf("job id is required: %w", ErrValidation)
	}
	if event.Type == "" {
		return fmt.Errorf("job event type is required: %w", ErrValidation)
	}
	if event.Message == "" {
		return fmt.Errorf("job event message is required: %w", ErrValidation)
	}
	return d.appendJobEvent(ctx, event.JobID, event.Type, event.Message, event.Details)
}
