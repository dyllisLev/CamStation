package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"camstation/internal/store"
)

func spaHandler(files http.FileSystem) http.Handler {
	fileServer := http.FileServer(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || !strings.Contains(filepathBase(r.URL.Path), ".") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.URL.Path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if file, err := files.Open(strings.TrimPrefix(r.URL.Path, "/")); err == nil {
			_ = file.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func filepathBase(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

type previewRegistry struct {
	mu      sync.Mutex
	streams map[string]previewStream
}

type previewStream struct {
	URL              string
	CameraStreamName string
	ExpiresAt        time.Time
}

func newPreviewRegistry() *previewRegistry {
	return &previewRegistry{streams: make(map[string]previewStream)}
}

func (p *previewRegistry) Put(rawURL string, ttl time.Duration) (string, time.Time) {
	return p.PutForCamera(rawURL, "", ttl)
}

func (p *previewRegistry) PutForCamera(rawURL, cameraStreamName string, ttl time.Duration) (string, time.Time) {
	expiresAt := time.Now().Add(ttl)
	name := "camstation-preview-" + randomToken()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streams[name] = previewStream{URL: rawURL, CameraStreamName: cameraStreamName, ExpiresAt: expiresAt}
	p.cleanupLocked(time.Now())
	return name, expiresAt
}

func (p *previewRegistry) Resolve(streamName string) (previewStream, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.cleanupLocked(now)
	stream, ok := p.streams[streamName]
	if !ok || now.After(stream.ExpiresAt) {
		return previewStream{}, false
	}
	return stream, true
}

func (p *previewRegistry) cleanupLocked(now time.Time) {
	for name, stream := range p.streams {
		if now.After(stream.ExpiresAt) {
			delete(p.streams, name)
		}
	}
}

func randomToken() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

type previewURLContextKey struct{}

func go2RTCProxy(previews *previewRegistry, registered func(context.Context, string) bool) (http.Handler, error) {
	target, err := url.Parse("http://127.0.0.1:1984")
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		r.Host = target.Host
		if r.URL.Path == "/api/ws" {
			r.Header.Set("Origin", target.String())
			if rawURL, ok := r.Context().Value(previewURLContextKey{}).(string); ok {
				query := r.URL.Query()
				query.Set("src", rawURL)
				r.URL.RawQuery = query.Encode()
			}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedGo2RTCPath(r.URL.Path) {
			writeError(w, http.StatusForbidden, fmt.Errorf("go2rtc endpoint is not exposed"))
			return
		}
		if r.URL.Path == "/api/ws" {
			if !isPlayerOriginAllowed(r) {
				writeError(w, http.StatusForbidden, fmt.Errorf("player origin is not allowed"))
				return
			}
			sources := r.URL.Query()["src"]
			if len(sources) != 1 || sources[0] == "" {
				writeError(w, http.StatusForbidden, fmt.Errorf("player stream is not allowed"))
				return
			}
			if preview, ok := previews.Resolve(sources[0]); ok {
				if preview.CameraStreamName != "" && (registered == nil || !registered(r.Context(), preview.CameraStreamName)) {
					writeError(w, http.StatusForbidden, fmt.Errorf("player stream is not allowed"))
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), previewURLContextKey{}, preview.URL))
			} else if registered == nil || !registered(r.Context(), sources[0]) {
				writeError(w, http.StatusForbidden, fmt.Errorf("player stream is not allowed"))
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

func isPlayerOriginAllowed(r *http.Request) bool {
	origin, err := url.Parse(strings.TrimSpace(r.Header.Get("Origin")))
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") {
		return false
	}
	return origin.Host == r.Host
}

func isRegisteredPublicStream(cameras []store.Camera, streamName string) bool {
	if streamName == "" {
		return false
	}
	for _, camera := range cameras {
		if !camera.Enabled {
			continue
		}
		for _, publicName := range []string{camera.StreamName, camera.RecordingStreamName, camera.LiveStreamName, camera.FocusStreamName} {
			if streamName == publicName && publicName != "" {
				return true
			}
		}
		for _, output := range camera.Outputs {
			if streamName == output.StreamName && output.StreamName != "" {
				return true
			}
		}
	}
	return false
}

func allowedGo2RTCPath(path string) bool {
	switch {
	case path == "/" || path == "/stream.html":
		return true
	case path == "/video-stream.js" || path == "/video-rtc.js":
		return true
	case path == "/api/ws":
		return true
	case strings.HasPrefix(path, "/icons/"):
		return true
	default:
		return false
	}
}
