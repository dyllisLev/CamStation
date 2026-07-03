package main

import "net/http"

func (d routeDeps) registerCameraRoutes(mux *http.ServeMux, previews *previewRegistry) {
	d.registerCameraProfileRoutes(mux, previews)
	d.registerCameraMutationRoutes(mux)
}
