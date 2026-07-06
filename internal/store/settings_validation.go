package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"camstation/internal/cronexpr"
)

func validateRecordingSettings(settings RecordingSettings) error {
	if settings.SegmentMinutes <= 0 {
		return fmt.Errorf("%w: recording segment minutes must be positive", ErrValidation)
	}
	if settings.RetentionDays < 0 {
		return fmt.Errorf("%w: recording retention days cannot be negative", ErrValidation)
	}
	if settings.MaxStorageGB < 0 {
		return fmt.Errorf("%w: recording max storage cannot be negative", ErrValidation)
	}
	return nil
}

func validateBackupSettings(settings BackupSettings) error {
	if strings.TrimSpace(settings.Target) == "" {
		return fmt.Errorf("%w: backup target is required", ErrValidation)
	}
	if settings.RetentionDays < 0 {
		return fmt.Errorf("%w: backup retention days cannot be negative", ErrValidation)
	}
	if !validCronExpression(settings.ScheduleCron) {
		return fmt.Errorf("%w: backup schedule cron is invalid", ErrValidation)
	}
	if settings.ScheduleEnabled && !settings.Enabled {
		return fmt.Errorf("%w: backup schedule requires backup to be enabled", ErrValidation)
	}
	return nil
}

func normalizeSettingsPayload(payload settingsPayload, rawJSON string) settingsPayload {
	defaults := defaultSettingsPayload()
	if payload.Recording.SegmentMinutes == 0 {
		payload.Recording.SegmentMinutes = defaults.Recording.SegmentMinutes
	}
	if payload.Backup.Target == "" {
		payload.Backup.Target = defaults.Backup.Target
	}
	if strings.TrimSpace(payload.Backup.ScheduleCron) == "" {
		payload.Backup.ScheduleCron = defaults.Backup.ScheduleCron
	}
	if !strings.Contains(rawJSON, `"protectUnbacked"`) {
		payload.Backup.ProtectUnbacked = defaults.Backup.ProtectUnbacked
	}
	return payload
}

func validCronExpression(expression string) bool {
	_, err := cronexpr.Parse(expression)
	return err == nil
}

func validateDiscordWebhookURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid Discord webhook URL", ErrValidation)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: Discord webhook must use https", ErrValidation)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "discord.com" && host != "discordapp.com" {
		return fmt.Errorf("%w: Discord webhook host is not allowed", ErrValidation)
	}
	if !strings.HasPrefix(parsed.EscapedPath(), "/api/webhooks/") {
		return fmt.Errorf("%w: Discord webhook path is not allowed", ErrValidation)
	}
	return nil
}

func secretDisplay(secret string) SecretDisplay {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return SecretDisplay{}
	}
	return SecretDisplay{
		HasSecret:   true,
		Masked:      maskSecret(secret),
		Fingerprint: secretFingerprint(secret),
	}
}

func maskSecret(secret string) string {
	if len(secret) <= 12 {
		return "********"
	}
	return secret[:6] + "..." + secret[len(secret)-6:]
}

func secretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:8])
}

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}
