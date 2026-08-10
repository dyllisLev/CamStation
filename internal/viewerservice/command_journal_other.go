//go:build !windows

package viewerservice

import "os"

func secureCommandJournalFile(path string) error {
	return os.Chmod(path, 0o600)
}
