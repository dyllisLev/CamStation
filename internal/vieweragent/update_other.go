//go:build !windows

package vieweragent

import "errors"

func verifyAuthenticode(_ string, _ string, allowUnsigned bool) error {
	if allowUnsigned {
		return nil
	}
	return errors.New("Authenticode verification requires Windows")
}

func launchUpdateDetached(string, []string) error {
	return errors.New("detached Windows updater requires Windows")
}
