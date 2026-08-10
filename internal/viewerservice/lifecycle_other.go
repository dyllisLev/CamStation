//go:build !windows

package viewerservice

func newPlatformViewerLauncher() ViewerLauncher { return nil }

func newPlatformServiceRestarter() ServiceRestarter { return nil }
