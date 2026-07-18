//go:build !windows

package viewerservice

import "context"

func (RegistryStore) Load(context.Context) (MachineConfig, error) {
	return MachineConfig{}, ErrUnsupportedPlatform
}

func (RegistryStore) Save(context.Context, MachineConfig) error {
	return ErrUnsupportedPlatform
}
