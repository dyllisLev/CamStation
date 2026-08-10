package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyViewerEntryRedirectsToLiveWorkspace(t *testing.T) {
	t.Parallel()

	server := newTestRouteServer(t)
	req := httptest.NewRequest(http.MethodGet, "/new?viewer=1", nil)
	rec := httptest.NewRecorder()

	server.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != legacyViewerLiveRoute {
		t.Fatalf("Location = %q, want %q", location, legacyViewerLiveRoute)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
}

func TestLegacyViewerEntryRejectsBroaderQueries(t *testing.T) {
	t.Parallel()

	server := newTestRouteServer(t)
	for _, target := range []string{
		"/new",
		"/new?viewer=",
		"/new?viewer=0",
		"/new?viewer=1&viewer=1",
		"/new?viewer=1&next=%2Flive",
	} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()

			server.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("GET %s status = %d, want %d; body=%s", target, rec.Code, http.StatusFound, rec.Body.String())
			}
			if location := rec.Header().Get("Location"); location != "/" {
				t.Fatalf("GET %s Location = %q, want /", target, location)
			}
		})
	}
}

func TestLegacyViewerEntryRejectsNonGETMethods(t *testing.T) {
	t.Parallel()

	server := newTestRouteServer(t)
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/new?viewer=1", nil)
			rec := httptest.NewRecorder()

			server.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d, want %d; body=%s", method, rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
			}
			if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
				t.Fatalf("%s Allow = %q, want GET", method, allow)
			}
		})
	}
}

func TestLegacyViewerCompatibilityRouteDoesNotCaptureSubpaths(t *testing.T) {
	t.Parallel()

	server := newTestRouteServer(t)
	req := httptest.NewRequest(http.MethodGet, "/new/recordings?viewer=1", nil)
	rec := httptest.NewRecorder()

	server.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want no server redirect", location)
	}
}
