package main

import (
	"context"
	"fmt"

	"camstation/internal/store"
)

const bytesPerGB int64 = 1024 * 1024 * 1024

func recordingStorageLimitBytes(ctx context.Context, db *store.DB, fallbackBytes int64) (int64, error) {
	settings, err := db.GetSettings(ctx)
	if err != nil {
		return 0, fmt.Errorf("load recording storage settings: %w", err)
	}
	return recordingStorageLimitBytesFromSettings(settings, fallbackBytes), nil
}

func recordingStorageLimitBytesFromSettings(settings store.Settings, fallbackBytes int64) int64 {
	if settings.Recording.MaxStorageGB > 0 {
		return gbToBytes(settings.Recording.MaxStorageGB)
	}
	if fallbackBytes > 0 {
		return fallbackBytes
	}
	return 0
}

func gbToBytes(gb float64) int64 {
	return int64(gb * float64(bytesPerGB))
}
