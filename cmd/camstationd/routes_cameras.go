package main

import "net/http"

func (d routeDeps) registerCameraRoutes(mux *http.ServeMux, previews *previewRegistry) {
	d.registerCameraProfileTemplateRoutes(mux)
	d.registerCameraProfileRoutes(mux, previews)
	d.registerCameraMutationRoutes(mux)
	d.registerCameraActivationRoutes(mux)
	d.registerCameraStreamOutputRoutes(mux)
	d.registerCameraControlRoutes(mux)
}
