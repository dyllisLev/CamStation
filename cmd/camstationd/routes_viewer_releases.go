package main

import (
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"camstation/internal/viewerrelease"
)

type viewerReleaseResponse struct {
	viewerrelease.Release
	DownloadURL string `json:"downloadUrl"`
}

func (d routeDeps) registerViewerReleaseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/viewers/app/version", d.handleViewerReleaseVersion)
	mux.HandleFunc("GET /api/viewers/app/download", d.handleViewerReleaseDownload)
}

func (d routeDeps) handleViewerReleaseVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, err := viewerrelease.Load(filepath.Join(d.viewerReleasesDir, "current"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	writeJSON(w, http.StatusOK, viewerReleaseResponse{Release: release, DownloadURL: release.DownloadURL()})
}

func (d routeDeps) handleViewerReleaseDownload(w http.ResponseWriter, _ *http.Request) {
	release, err := viewerrelease.Load(filepath.Join(d.viewerReleasesDir, "current"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	file, err := release.OpenVerified()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Disposition", attachmentDisposition(release.Filename))
	w.Header().Set("Content-Type", "application/vnd.microsoft.portable-executable")
	w.Header().Set("Content-Length", strconv.FormatInt(release.SizeBytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func attachmentDisposition(filename string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(filename)
	return `attachment; filename="` + escaped + `"`
}
