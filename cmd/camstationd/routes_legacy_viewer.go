package main

import "net/http"

const legacyViewerLiveRoute = "/live?viewer=1"

func (d routeDeps) registerLegacyViewerCompatibilityRoute(mux *http.ServeMux) {
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		query := r.URL.Query()
		viewerValues, hasViewer := query["viewer"]
		if len(query) == 1 && hasViewer && len(viewerValues) == 1 && viewerValues[0] == "1" {
			http.Redirect(w, r, legacyViewerLiveRoute, http.StatusFound)
			return
		}

		http.Redirect(w, r, "/", http.StatusFound)
	})
}
