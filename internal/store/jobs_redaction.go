package store

import (
	"regexp"
	"strings"
)

var discordWebhookPattern = regexp.MustCompile(`(?i)https?://(?:ptb\.|canary\.)?(?:discord(?:app)?\.com)/api/webhooks/[^\s"'<>]+`)
var credentialURLPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s"'<>@]+:[^@\s"'<>]+@`)

func RedactText(value string) string {
	value = credentialURLPattern.ReplaceAllString(value, "${1}redacted:redacted@")
	return discordWebhookPattern.ReplaceAllString(value, "[redacted]")
}

func sanitizeJob(job Job) Job {
	job.Kind = sanitizeJobString(job.Kind)
	job.SingleFlightKey = sanitizeJobString(job.SingleFlightKey)
	job.Error = sanitizeJobString(job.Error)
	job.Result = sanitizeJobPayload(job.Result)
	for index := range job.Events {
		job.Events[index] = sanitizeJobEvent(job.Events[index])
	}
	return job
}

func sanitizeJobEvent(event JobEvent) JobEvent {
	event.Message = sanitizeJobString(event.Message)
	event.Details = sanitizeJobPayload(event.Details)
	return event
}

func sanitizeJobPayload(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	sanitized := make(map[string]any, len(value))
	for key, item := range value {
		safeKey := sanitizeJobString(key)
		if redactsSecretKey(key) {
			sanitized[safeKey] = "[redacted]"
			continue
		}
		sanitized[safeKey] = sanitizeJobValue(item)
	}
	return sanitized
}

func sanitizeJobValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeJobString(typed)
	case map[string]any:
		return sanitizeJobPayload(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, sanitizeJobValue(item))
		}
		return items
	default:
		return typed
	}
}

func sanitizeJobString(value string) string {
	return RedactText(value)
}

func jobStringContainsSecret(value string) bool {
	return sanitizeJobString(value) != value
}

func redactsSecretKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "webhook")
}
