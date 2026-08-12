package cameracontrol

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"testing"

	"camstation/internal/onvif"
	"camstation/internal/store"
)

func TestControllerStopIsFinalWireCommand(t *testing.T) {
	moveStarted := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var actions []string
	controller := newWithCall(func(ctx context.Context, _ onvif.Target, _ onvif.Service, action, _ string) (string, error) {
		mu.Lock()
		actions = append(actions, path.Base(action))
		mu.Unlock()
		if strings.HasSuffix(action, "/ContinuousMove") {
			once.Do(func() { close(moveStarted) })
			<-ctx.Done()
			return "", ctx.Err()
		}
		return `<Envelope><Body/></Envelope>`, nil
	})
	camera := controlTestCamera()
	moveDone := make(chan error, 1)
	go func() { moveDone <- controller.Move(context.Background(), camera, MoveVector{Pan: .4}) }()
	<-moveStarted
	if err := controller.Stop(context.Background(), camera); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	_ = <-moveDone
	mu.Lock()
	defer mu.Unlock()
	if got := actions[len(actions)-1]; got != "Stop" {
		t.Fatalf("last action = %q, want Stop; all=%v", got, actions)
	}
}

func TestControllerParsesCapabilitiesStatusAndPresets(t *testing.T) {
	controller := controllerWithFixtureResponses(t, map[string]string{
		"GetNodes":        ptzNodeFixture(true, 100),
		"GetAudioSources": audioSourcesFixture(1),
		"GetStatus":       ptzStatusFixture("IDLE", "IDLE"),
		"GetPresets":      presetsFixture("preset-1", "입구"),
	})
	camera := controlTestCamera()
	caps, err := controller.Discover(t.Context(), camera)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !caps.PTZ.Available || !caps.Home.Available || !caps.Presets.Available || caps.MaxPresets != 100 {
		t.Fatalf("capabilities = %#v", caps)
	}
	if caps.Listen.Support != store.ControlSupportSupported || caps.Listen.Available || caps.DiscoveredAt == "" {
		t.Fatalf("listen/discoveredAt = %#v/%q", caps.Listen, caps.DiscoveredAt)
	}
	status, err := controller.Status(t.Context(), camera)
	if err != nil || status.PanTilt != "IDLE" || status.Zoom != "IDLE" {
		t.Fatalf("status/error = %#v/%v", status, err)
	}
	presets, err := controller.ListPresets(t.Context(), camera)
	if err != nil || len(presets) != 1 || presets[0].Token != "preset-1" || presets[0].Name != "입구" {
		t.Fatalf("presets/error = %#v/%v", presets, err)
	}
}

func TestControllerEscapesPresetNameAndToken(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]string{}
	controller := newWithCall(func(_ context.Context, _ onvif.Target, _ onvif.Service, action, body string) (string, error) {
		operation := path.Base(action)
		mu.Lock()
		bodies[operation] = body
		mu.Unlock()
		if operation == "SetPreset" {
			return `<Envelope><Body><SetPresetResponse><PresetToken>created</PresetToken></SetPresetResponse></Body></Envelope>`, nil
		}
		return `<Envelope><Body/></Envelope>`, nil
	})
	camera := controlTestCamera()
	if _, err := controller.CreatePreset(t.Context(), camera, ` A&B<입구> `); err != nil {
		t.Fatalf("CreatePreset: %v", err)
	}
	for _, call := range []func() error{
		func() error { return controller.GotoPreset(t.Context(), camera, `preset/a?b&c`) },
		func() error { return controller.DeletePreset(t.Context(), camera, `preset/a?b&c`) },
	} {
		if err := call(); err != nil {
			t.Fatalf("preset token action: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(bodies["SetPreset"], `A&amp;B&lt;입구&gt;`) {
		t.Fatalf("unescaped preset name: %s", bodies["SetPreset"])
	}
	for _, operation := range []string{"GotoPreset", "RemovePreset"} {
		if !strings.Contains(bodies[operation], `preset/a?b&amp;c`) {
			t.Fatalf("unescaped %s token: %s", operation, bodies[operation])
		}
	}
}

func TestControllerClampsMoveVectorsAndSetsDeviceTimeout(t *testing.T) {
	var body string
	controller := newWithCall(func(_ context.Context, _ onvif.Target, _ onvif.Service, _ string, value string) (string, error) {
		body = value
		return `<Envelope><Body/></Envelope>`, nil
	})
	if err := controller.Move(t.Context(), controlTestCamera(), MoveVector{Pan: 3, Tilt: -4, Zoom: .5}); err != nil {
		t.Fatalf("Move: %v", err)
	}
	for _, expected := range []string{`x="1" y="-1"`, `Zoom x="0.5"`, `PT2S`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("move body missing %q: %s", expected, body)
		}
	}
	if err := controller.Move(t.Context(), controlTestCamera(), MoveVector{}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("zero move error = %v", err)
	}
}

func TestTargetForCameraUsesURLCredentialsAndRecordingToken(t *testing.T) {
	target, token, err := targetForCamera(controlTestCamera())
	if err != nil {
		t.Fatalf("targetForCamera: %v", err)
	}
	if target.Username != "operator" || target.Password != "camera-secret" || target.Host != "192.0.2.10" || target.Port != 80 {
		t.Fatalf("target = %#v", target)
	}
	if token != "PROFILE_000" {
		t.Fatalf("token = %q", token)
	}
}

func TestTargetForCameraRejectsMissingProfileToken(t *testing.T) {
	camera := controlTestCamera()
	camera.Streams = nil
	_, _, err := targetForCamera(camera)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestControllerErrorsDoNotContainCredentials(t *testing.T) {
	controller := newWithCall(func(context.Context, onvif.Target, onvif.Service, string, string) (string, error) {
		return "", errors.New("camera-secret at http://192.0.2.10/onvif")
	})
	err := controller.Move(t.Context(), controlTestCamera(), MoveVector{Pan: .2})
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "camera-secret") || strings.Contains(err.Error(), "192.0.2.10") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func controlTestCamera() store.Camera {
	return store.Camera{
		Name: "염소장", StreamName: "goat-yard", Host: "192.0.2.10", ONVIFPort: 80,
		URL: "rtsp://operator:camera-secret@192.0.2.10/live",
		Streams: []store.CameraStream{
			{Role: store.CameraStreamRoleRecording, ProfileToken: "PROFILE_000"},
			{Role: store.CameraStreamRoleLive, ProfileToken: "PROFILE_001"},
		},
	}
}

func controllerWithFixtureResponses(t *testing.T, responses map[string]string) *Controller {
	t.Helper()
	return newWithCall(func(_ context.Context, _ onvif.Target, _ onvif.Service, action, _ string) (string, error) {
		operation := path.Base(action)
		response, ok := responses[operation]
		if !ok {
			t.Fatalf("unexpected ONVIF operation %q", operation)
		}
		return response, nil
	})
}

func ptzNodeFixture(home bool, maxPresets int) string {
	return fmt.Sprintf(`<Envelope><Body><GetNodesResponse><PTZNode token="node-1"><HomeSupported>%t</HomeSupported><MaximumNumberOfPresets>%d</MaximumNumberOfPresets><SupportedPTZSpaces><ContinuousPanTiltVelocitySpace/><ContinuousZoomVelocitySpace/></SupportedPTZSpaces></PTZNode></GetNodesResponse></Body></Envelope>`, home, maxPresets)
}

func audioSourcesFixture(count int) string {
	var sources strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&sources, `<AudioSources token="audio-%d"/>`, index)
	}
	return `<Envelope><Body><GetAudioSourcesResponse>` + sources.String() + `</GetAudioSourcesResponse></Body></Envelope>`
}

func ptzStatusFixture(panTilt, zoom string) string {
	return fmt.Sprintf(`<Envelope><Body><GetStatusResponse><PTZStatus><Position><PanTilt x=".2" y=".3"/></Position><MoveStatus><PanTilt>%s</PanTilt><Zoom>%s</Zoom></MoveStatus></PTZStatus></GetStatusResponse></Body></Envelope>`, panTilt, zoom)
}

func presetsFixture(token, name string) string {
	return fmt.Sprintf(`<Envelope><Body><GetPresetsResponse><Preset token="%s"><Name>%s</Name></Preset></GetPresetsResponse></Body></Envelope>`, token, name)
}
