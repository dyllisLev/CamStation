package stream

import (
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestLiveWarmSpecsSelectEnabledPublicLiveExactlyOnce(t *testing.T) {
	cameras := []store.Camera{
		{
			ID: 1, Enabled: true,
			Outputs: []store.CameraOutput{
				{Purpose: store.CameraOutputRecording, StreamName: "one-recording"},
				{Purpose: store.CameraOutputLive, StreamName: "소방서4-live"},
				{Purpose: store.CameraOutputLive, StreamName: "소방서4-live"},
			},
		},
		{ID: 2, Enabled: false, Outputs: []store.CameraOutput{{Purpose: store.CameraOutputLive, StreamName: "disabled-live"}}},
		{ID: 3, Enabled: true, Outputs: []store.CameraOutput{{Purpose: store.CameraOutputFocus, StreamName: "focus-only"}}},
	}

	got := liveWarmSpecs(cameras, "rtsp://127.0.0.1:8554")
	want := []liveWarmSpec{{StreamName: "소방서4-live", Input: "rtsp://127.0.0.1:8554/%EC%86%8C%EB%B0%A9%EC%84%9C4-live"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("specs = %#v, want %#v", got, want)
	}
}

func TestBuildLiveWarmFFmpegArgsConsumesVideoWithoutEncoding(t *testing.T) {
	input := "rtsp://127.0.0.1:8554/camera-live"
	got := BuildLiveWarmFFmpegArgs(input)
	want := []string{
		"ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-nostats",
		"-rtsp_transport", "tcp", "-user_agent", "CamStationWarm/2.0", "-i", input,
		"-map", "0:v:0", "-c:v", "copy", "-an", "-f", "null", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestLiveWarmManagerStartsAllStreamsConcurrently(t *testing.T) {
	started := make(chan string, 8)
	runner := func(stop <-chan struct{}, spec liveWarmSpec, setActive func(bool)) error {
		setActive(true)
		started <- spec.StreamName
		<-stop
		setActive(false)
		return nil
	}
	manager := newLiveWarmManager(withLiveWarmRunner(runner))
	t.Cleanup(manager.StopAll)

	manager.Reconcile(eightLiveWarmCameras())
	seen := make([]string, 0, 8)
	deadline := time.After(time.Second)
	for len(seen) < 8 {
		select {
		case name := <-started:
			seen = append(seen, name)
		case <-deadline:
			t.Fatalf("only %d warm workers started; starts were not independent", len(seen))
		}
	}
	sort.Strings(seen)
	want := []string{"camera-1-live", "camera-2-live", "camera-3-live", "camera-4-live", "camera-5-live", "camera-6-live", "camera-7-live", "camera-8-live"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("started = %#v, want %#v", seen, want)
	}

	snapshot := manager.Snapshot()
	if len(snapshot.Expected) != 8 || len(snapshot.Active) != 8 {
		t.Fatalf("snapshot = %#v, want expected=8 active=8", snapshot)
	}
}

func TestLiveWarmManagerRetriesOnlyExitedWorker(t *testing.T) {
	var mu sync.Mutex
	attempts := map[string]int{}
	secondAttempt := make(chan struct{})
	runner := func(stop <-chan struct{}, spec liveWarmSpec, setActive func(bool)) error {
		mu.Lock()
		attempts[spec.StreamName]++
		attempt := attempts[spec.StreamName]
		mu.Unlock()
		if spec.StreamName == "camera-1-live" && attempt == 1 {
			return errors.New("synthetic exit")
		}
		if spec.StreamName == "camera-1-live" && attempt == 2 {
			close(secondAttempt)
		}
		setActive(true)
		<-stop
		setActive(false)
		return nil
	}
	manager := newLiveWarmManager(
		withLiveWarmRunner(runner),
		withLiveWarmRetry(5*time.Millisecond, 10*time.Millisecond),
	)
	t.Cleanup(manager.StopAll)
	manager.Reconcile(eightLiveWarmCameras()[:2])

	select {
	case <-secondAttempt:
	case <-time.After(time.Second):
		t.Fatal("exited worker was not retried")
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts["camera-1-live"] != 2 || attempts["camera-2-live"] != 1 {
		t.Fatalf("attempts = %#v, want failed=2 unaffected=1", attempts)
	}
}

func TestLiveReadinessRequiresActiveWarmConsumerAndProducer(t *testing.T) {
	snapshot := LiveWarmSnapshot{
		Expected: []string{"one-live", "two-live"},
		Active:   map[string]bool{"one-live": true},
	}
	runtime := map[string]StreamRuntime{
		"one-live": {ProducerCount: 1, ConsumerCount: 1},
		"two-live": {ProducerCount: 1, ConsumerCount: 1},
	}
	expected, ready, mediaReady := liveReadiness(snapshot, runtime)
	if expected != 2 || ready != 1 || mediaReady {
		t.Fatalf("readiness = expected:%d ready:%d media:%v", expected, ready, mediaReady)
	}
	snapshot.Active["two-live"] = true
	expected, ready, mediaReady = liveReadiness(snapshot, runtime)
	if expected != 2 || ready != 2 || !mediaReady {
		t.Fatalf("readiness = expected:%d ready:%d media:%v", expected, ready, mediaReady)
	}
}

func eightLiveWarmCameras() []store.Camera {
	cameras := make([]store.Camera, 0, 8)
	for i := 1; i <= 8; i++ {
		name := "camera-" + string(rune('0'+i)) + "-live"
		cameras = append(cameras, store.Camera{
			ID:      int64(i),
			Enabled: true,
			Outputs: []store.CameraOutput{{Purpose: store.CameraOutputLive, StreamName: name}},
		})
	}
	return cameras
}
