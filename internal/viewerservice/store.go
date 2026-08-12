package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxRegistryDocumentBytes = 64 * 1024

var (
	ErrNotConfigured          = errors.New("viewer is not configured")
	ErrUnsupportedPlatform    = errors.New("viewer configuration registry is unsupported on this platform")
	ErrRegistryAccess         = errors.New("viewer configuration registry access failed")
	ErrConfigDecode           = errors.New("viewer configuration decode failed")
	ErrUnsupportedSchema      = errors.New("unsupported viewer configuration schema")
	ErrInvalidPersistedConfig = errors.New("invalid persisted viewer configuration")
	ErrConfigTooLarge         = errors.New("viewer configuration exceeds 64 KiB")
)

type ConfigStore interface {
	Load(context.Context) (MachineConfig, error)
	Save(context.Context, MachineConfig) error
}

type RegistryStore struct{}

func loadOrEmpty(ctx context.Context, store ConfigStore) (MachineConfig, error) {
	if store == nil {
		return MachineConfig{}, errors.New("configuration store is unavailable")
	}
	config, err := store.Load(ctx)
	if errors.Is(err, ErrNotConfigured) {
		return MachineConfig{}, nil
	}
	return config, err
}

func encodeRegistryDocument(config MachineConfig) (string, error) {
	config, err := validatePersistedConfig(config)
	if err != nil {
		return "", err
	}
	document, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrConfigDecode, err)
	}
	if len(document) > maxRegistryDocumentBytes {
		return "", ErrConfigTooLarge
	}
	return string(document), nil
}

func decodeRegistryDocument(document string) (MachineConfig, error) {
	if len(document) > maxRegistryDocumentBytes {
		return MachineConfig{}, ErrConfigTooLarge
	}
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.DisallowUnknownFields()
	var config MachineConfig
	if err := decoder.Decode(&config); err != nil {
		return MachineConfig{}, fmt.Errorf("%w: %w", ErrConfigDecode, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return MachineConfig{}, err
	}
	return validatePersistedConfig(config)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: multiple JSON values", ErrConfigDecode)
		}
		return fmt.Errorf("%w: %w", ErrConfigDecode, err)
	}
	return nil
}

func validatePersistedConfig(config MachineConfig) (MachineConfig, error) {
	if config.SchemaVersion != ConfigSchemaVersion {
		return MachineConfig{}, fmt.Errorf("%w: got %d", ErrUnsupportedSchema, config.SchemaVersion)
	}
	serverURL, err := validateServerURL(config.ServerURL)
	if err != nil {
		return MachineConfig{}, fmt.Errorf("%w: %w", ErrInvalidPersistedConfig, err)
	}
	displayName, err := validateDisplayName(config.DisplayName)
	if err != nil {
		return MachineConfig{}, fmt.Errorf("%w: %w", ErrInvalidPersistedConfig, err)
	}
	if strings.TrimSpace(config.ClientID) == "" || containsControl(config.ClientID) {
		return MachineConfig{}, fmt.Errorf("%w: client ID is required", ErrInvalidPersistedConfig)
	}
	config.ServerURL = serverURL
	config.DisplayName = displayName
	return config, nil
}
