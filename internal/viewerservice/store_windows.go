//go:build windows

package viewerservice

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

func (RegistryStore) Load(ctx context.Context) (MachineConfig, error) {
	if err := ctx.Err(); err != nil {
		return MachineConfig{}, err
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, RegistrySubkey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return MachineConfig{}, ErrNotConfigured
	}
	if err != nil {
		return MachineConfig{}, fmt.Errorf("%w: open key: %w", ErrRegistryAccess, err)
	}
	defer key.Close()

	document, valueType, err := key.GetStringValue(RegistryValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return MachineConfig{}, ErrNotConfigured
	}
	if errors.Is(err, registry.ErrUnexpectedType) || (err == nil && valueType != registry.SZ) {
		return MachineConfig{}, fmt.Errorf("%w: registry value is not REG_SZ", ErrConfigDecode)
	}
	if err != nil {
		return MachineConfig{}, fmt.Errorf("%w: read value: %w", ErrRegistryAccess, err)
	}
	return decodeRegistryDocument(document)
}

func (RegistryStore) Save(ctx context.Context, config MachineConfig) error {
	document, err := encodeRegistryDocument(config)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, RegistrySubkey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return ErrNotConfigured
	}
	if err != nil {
		return fmt.Errorf("%w: open key: %w", ErrRegistryAccess, err)
	}
	defer key.Close()
	if err := key.SetStringValue(RegistryValueName, document); err != nil {
		return fmt.Errorf("%w: write value: %w", ErrRegistryAccess, err)
	}
	return nil
}
