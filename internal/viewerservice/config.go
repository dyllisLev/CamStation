package viewerservice

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

const (
	ConfigSchemaVersion = 1
	RegistrySubkey      = `Software\CamStation\Viewer`
	RegistryValueName   = `Configuration`

	CodeInvalidInput         = "invalid_input"
	CodeServerUnreachable    = "server_unreachable"
	CodeAPIIncompatible      = "api_incompatible"
	CodeRegistrationRejected = "registration_rejected"
	CodeStorageFailed        = "storage_failed"
)

var (
	ErrInvalidInput         = errors.New(CodeInvalidInput)
	ErrServerUnreachable    = errors.New(CodeServerUnreachable)
	ErrAPIIncompatible      = errors.New(CodeAPIIncompatible)
	ErrRegistrationRejected = errors.New(CodeRegistrationRejected)
	ErrStorageFailed        = errors.New(CodeStorageFailed)
)

type MachineConfig struct {
	SchemaVersion int    `json:"schemaVersion"`
	ServerURL     string `json:"serverUrl"`
	DisplayName   string `json:"displayName"`
	ClientID      string `json:"clientId"`
	AutoStart     bool   `json:"autoStart"`
}

type ConfigDraft struct {
	ServerURL   string `json:"serverUrl"`
	DisplayName string `json:"displayName"`
	AutoStart   bool   `json:"autoStart"`
}

type ConnectionValidator interface {
	Validate(context.Context, ConfigDraft, string) error
}

type ConfigManager struct {
	Store     ConfigStore
	Validator ConnectionValidator
	NewID     func() (string, error)
}

func BuildConfig(draft ConfigDraft, current MachineConfig, newID func() (string, error)) (MachineConfig, error) {
	serverURL, err := validateServerURL(draft.ServerURL)
	if err != nil {
		return MachineConfig{}, err
	}
	displayName, err := validateDisplayName(draft.DisplayName)
	if err != nil {
		return MachineConfig{}, err
	}
	clientID := current.ClientID
	if strings.TrimSpace(clientID) == "" {
		if newID == nil {
			return MachineConfig{}, fmt.Errorf("%w: client ID generator is unavailable", ErrStorageFailed)
		}
		clientID, err = newID()
		if err != nil {
			return MachineConfig{}, fmt.Errorf("%w: generate client ID: %w", ErrStorageFailed, err)
		}
	}
	if strings.TrimSpace(clientID) == "" || containsControl(clientID) {
		return MachineConfig{}, fmt.Errorf("%w: generated client ID is invalid", ErrStorageFailed)
	}
	return MachineConfig{
		SchemaVersion: ConfigSchemaVersion,
		ServerURL:     serverURL,
		DisplayName:   displayName,
		ClientID:      clientID,
		AutoStart:     draft.AutoStart,
	}, nil
}

func (manager ConfigManager) Commit(ctx context.Context, draft ConfigDraft) (MachineConfig, error) {
	current, err := loadOrEmpty(ctx, manager.Store)
	if err != nil {
		return MachineConfig{}, storageError(err)
	}
	candidate, err := BuildConfig(draft, current, manager.NewID)
	if err != nil {
		return MachineConfig{}, err
	}
	if manager.Validator == nil {
		return MachineConfig{}, fmt.Errorf("%w: connection validator is unavailable", ErrServerUnreachable)
	}
	if err := manager.Validator.Validate(ctx, draft, candidate.ClientID); err != nil {
		return MachineConfig{}, err
	}
	if err := manager.Store.Save(ctx, candidate); err != nil {
		return MachineConfig{}, storageError(err)
	}
	return candidate, nil
}

func ErrorCode(err error) string {
	for _, item := range []struct {
		err  error
		code string
	}{
		{ErrStorageFailed, CodeStorageFailed},
		{ErrInvalidInput, CodeInvalidInput},
		{ErrServerUnreachable, CodeServerUnreachable},
		{ErrAPIIncompatible, CodeAPIIncompatible},
		{ErrRegistrationRejected, CodeRegistrationRejected},
	} {
		if errors.Is(err, item.err) {
			return item.code
		}
	}
	return ""
}

func validateServerURL(raw string) (string, error) {
	if raw == "" || raw != strings.TrimSpace(raw) || containsControl(raw) {
		return "", fmt.Errorf("%w: server URL is required without whitespace", ErrInvalidInput)
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Opaque != "" {
		return "", fmt.Errorf("%w: server URL must be an absolute HTTP or HTTPS URL", ErrInvalidInput)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%w: server URL must not contain credentials", ErrInvalidInput)
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") {
		return "", fmt.Errorf("%w: server URL must not contain a path, query, or fragment", ErrInvalidInput)
	}
	parsed.Path = ""
	return parsed.String(), nil
}

func validateDisplayName(raw string) (string, error) {
	displayName := strings.TrimSpace(raw)
	if displayName == "" || containsControl(displayName) {
		return "", fmt.Errorf("%w: display name is required without control characters", ErrInvalidInput)
	}
	return displayName, nil
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func storageError(err error) error {
	if errors.Is(err, ErrStorageFailed) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrStorageFailed, err)
}
