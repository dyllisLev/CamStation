package main

import (
	"context"
	"net/http"
	"testing"

	"camstation/internal/store"
	"camstation/internal/stream"
)

func TestCameraActivationDisablesCameraAndReturnsPublicState(t *testing.T) {
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		return stream.PolicyApplyResult{Applied: true}
	}))

	status, payload := requestJSON(t, server.handler, http.MethodPatch, "/api/cameras/"+camera.StreamName+"/enabled", `{"enabled":false}`)
	if status != http.StatusOK || payload["applied"] != true {
		t.Fatalf("response=%d %#v", status, payload)
	}
	publicCamera, ok := payload["camera"].(map[string]any)
	if !ok || publicCamera["enabled"] != false {
		t.Fatalf("public camera=%#v", payload["camera"])
	}
	stored, err := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil || stored.Enabled {
		t.Fatalf("stored camera=%#v err=%v", stored, err)
	}
}

func TestCameraActivationRestoresStoredStateWhenApplyFails(t *testing.T) {
	calls := 0
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		calls++
		if calls == 1 {
			return stream.PolicyApplyResult{Pending: true, Error: "runtime unavailable"}
		}
		return stream.PolicyApplyResult{Applied: true}
	}))

	status, _ := requestJSON(t, server.handler, http.MethodPatch, "/api/cameras/"+camera.StreamName+"/enabled", `{"enabled":false}`)
	if status != http.StatusBadGateway {
		t.Fatalf("status=%d, want %d", status, http.StatusBadGateway)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil || !stored.Enabled || calls != 2 {
		t.Fatalf("stored=%#v calls=%d err=%v", stored, calls, err)
	}
}

func TestDisabledCameraConnectionRoutesFailBeforeNetworkUse(t *testing.T) {
	prober := &occupiedRTSPPolicyProber{}
	server, camera := newPolicyRouteServerWithProber(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		return stream.PolicyApplyResult{Applied: true}
	}), prober)
	if err := server.db.SetCameraEnabled(t.Context(), camera.StreamName, false); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/streams/" + camera.StreamName + "/probe", ""},
		{http.MethodPost, "/api/cameras/" + camera.StreamName + "/scan", `{}`},
		{http.MethodPost, "/api/cameras/" + camera.StreamName + "/preview", `{}`},
		{http.MethodGet, "/api/cameras/" + camera.StreamName + "/controls", ""},
		{http.MethodPost, "/api/cameras/" + camera.StreamName + "/stream-outputs/probe", ""},
	}
	for _, test := range tests {
		status, payload := requestJSON(t, server.handler, test.method, test.path, test.body)
		if status != http.StatusConflict {
			t.Fatalf("%s %s status=%d payload=%#v", test.method, test.path, status, payload)
		}
	}
	if len(prober.calls) != 0 {
		t.Fatalf("disabled camera reached prober: %v", prober.calls)
	}
}

func TestDisabledCameraUpdatePersistsWithoutProbe(t *testing.T) {
	prober := &occupiedRTSPPolicyProber{}
	server, camera := newPolicyRouteServerWithProber(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		return stream.PolicyApplyResult{Applied: true}
	}), prober)
	if err := server.db.SetCameraEnabled(t.Context(), camera.StreamName, false); err != nil {
		t.Fatal(err)
	}
	body := cameraSecurityCreateBody(t, camera.StreamName, routeSyntheticRTSPURL("disabled-update"))

	status, payload := requestJSON(t, server.handler, http.MethodPut, "/api/cameras/"+camera.StreamName, body)
	if status != http.StatusOK {
		t.Fatalf("status=%d payload=%#v", status, payload)
	}
	if len(prober.calls) != 0 {
		t.Fatalf("disabled camera update reached prober: %v", prober.calls)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil || stored.Enabled {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestDisabledCameraIsNotRegisteredPlayerStream(t *testing.T) {
	camera := store.Camera{Enabled: false, StreamName: "disabled", LiveStreamName: "disabled-live"}
	if isRegisteredPublicStream([]store.Camera{camera}, camera.LiveStreamName) {
		t.Fatal("disabled stream accepted by player proxy")
	}
}
