package main

import (
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
	URL       string
	ExpiresAt time.Time
}

func newPreviewRegistry() *previewRegistry {
	return &previewRegistry{streams: make(map[string]previewStream)}
}

func (p *previewRegistry) Put(rawURL string, ttl time.Duration) (string, time.Time) {
	expiresAt := time.Now().Add(ttl)
	name := "camstation-preview-" + randomToken()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streams[name] = previewStream{URL: rawURL, ExpiresAt: expiresAt}
	p.cleanupLocked(time.Now())
	return name, expiresAt
}

func (p *previewRegistry) Resolve(streamName string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.cleanupLocked(now)
	stream, ok := p.streams[streamName]
	if !ok || now.After(stream.ExpiresAt) {
		return "", false
	}
	return stream.URL, true
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

func go2RTCProxy(previews *previewRegistry) (http.Handler, error) {
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
			query := r.URL.Query()
			if rawURL, ok := previews.Resolve(query.Get("src")); ok {
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
		proxy.ServeHTTP(w, r)
	}), nil
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
