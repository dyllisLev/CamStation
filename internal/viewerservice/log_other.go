//go:build !windows

package viewerservice

func secureViewerLogFile(string, string) error {
	return ErrUnsupportedPlatform
}
