//go:build !windows

package vieweragent

import "os"

func openStateFile(path string) (*os.File, error) {
	return os.Open(path)
}
