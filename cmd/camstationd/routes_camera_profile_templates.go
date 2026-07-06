package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"camstation/internal/store"
)

func (d routeDeps) registerCameraProfileTemplateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/camera-profiles", func(w http.ResponseWriter, r *http.Request) {
		templates, err := d.db.ListCameraProfileTemplates(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, publicCameraProfileTemplates(templates))
	})

	mux.HandleFunc("POST /api/camera-profiles", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		template, ok := readCameraProfileTemplateRequest(w, r)
		if !ok {
			return
		}
		created, err := d.db.CreateCameraProfileTemplate(r.Context(), template)
		if err != nil {
			writeCameraProfileTemplateError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, publicCameraProfileTemplateFromStore(created))
	})

	mux.HandleFunc("GET /api/camera-profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := cameraProfileTemplateID(w, r)
		if !ok {
			return
		}
		template, err := d.db.GetCameraProfileTemplate(r.Context(), id)
		if err != nil {
			writeCameraProfileTemplateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, publicCameraProfileTemplateFromStore(template))
	})

	mux.HandleFunc("PUT /api/camera-profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		id, ok := cameraProfileTemplateID(w, r)
		if !ok {
			return
		}
		template, ok := readCameraProfileTemplateRequest(w, r)
		if !ok {
			return
		}
		updated, err := d.db.UpdateCameraProfileTemplate(r.Context(), id, template)
		if err != nil {
			writeCameraProfileTemplateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, publicCameraProfileTemplateFromStore(updated))
	})

	mux.HandleFunc("DELETE /api/camera-profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		id, ok := cameraProfileTemplateID(w, r)
		if !ok {
			return
		}
		if err := d.db.DeleteCameraProfileTemplate(r.Context(), id); err != nil {
			writeCameraProfileTemplateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	})
}

func readCameraProfileTemplateRequest(w http.ResponseWriter, r *http.Request) (store.CameraProfileTemplate, bool) {
	var req cameraProfileTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSafeError(w, http.StatusBadRequest, err)
		return store.CameraProfileTemplate{}, false
	}
	return req.storeTemplate(), true
}

func cameraProfileTemplateID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || id <= 0 {
		writeSafeError(w, http.StatusNotFound, fmt.Errorf("camera profile template id: %w", store.ErrNotFound))
		return 0, false
	}
	return id, true
}

func writeCameraProfileTemplateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeSafeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrProfileTemplateInvalid):
		writeSafeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrProfileTemplateDuplicate):
		writeSafeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrProfileTemplateInUse):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "camera profile template in use"})
	default:
		writeSafeError(w, http.StatusInternalServerError, err)
	}
}

type cameraProfileTemplateRequest struct {
	ProfileName  string                               `json:"profileName"`
	Manufacturer string                               `json:"manufacturer"`
	Model        string                               `json:"model"`
	Adapter      string                               `json:"adapter"`
	Version      int                                  `json:"version"`
	MatchRules   []store.CameraProfileMatchRule       `json:"matchRules"`
	Channels     []store.CameraProfileTemplateChannel `json:"channels"`
	Capabilities store.CameraProfileCapabilities      `json:"capabilities"`
}

func (r cameraProfileTemplateRequest) storeTemplate() store.CameraProfileTemplate {
	return store.CameraProfileTemplate{
		ProfileName:  r.ProfileName,
		Manufacturer: r.Manufacturer,
		Model:        r.Model,
		Adapter:      r.Adapter,
		Version:      r.Version,
		MatchRules:   r.MatchRules,
		Channels:     r.Channels,
		Capabilities: r.Capabilities,
	}
}

type publicCameraProfileTemplate struct {
	ID           int64                                `json:"id"`
	ProfileName  string                               `json:"profileName"`
	Manufacturer string                               `json:"manufacturer"`
	Model        string                               `json:"model"`
	Adapter      string                               `json:"adapter"`
	Version      int                                  `json:"version"`
	MatchRules   []store.CameraProfileMatchRule       `json:"matchRules"`
	Channels     []store.CameraProfileTemplateChannel `json:"channels"`
	Capabilities store.CameraProfileCapabilities      `json:"capabilities"`
	CreatedAt    time.Time                            `json:"createdAt"`
	UpdatedAt    time.Time                            `json:"updatedAt"`
}

func publicCameraProfileTemplates(templates []store.CameraProfileTemplate) []publicCameraProfileTemplate {
	out := make([]publicCameraProfileTemplate, 0, len(templates))
	for _, template := range templates {
		out = append(out, publicCameraProfileTemplateFromStore(template))
	}
	return out
}

func publicCameraProfileTemplateFromStore(template store.CameraProfileTemplate) publicCameraProfileTemplate {
	return publicCameraProfileTemplate{
		ID:           template.ID,
		ProfileName:  template.ProfileName,
		Manufacturer: template.Manufacturer,
		Model:        template.Model,
		Adapter:      template.Adapter,
		Version:      template.Version,
		MatchRules:   template.MatchRules,
		Channels:     template.Channels,
		Capabilities: template.Capabilities,
		CreatedAt:    template.CreatedAt,
		UpdatedAt:    template.UpdatedAt,
	}
}
