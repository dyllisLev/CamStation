package stream

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"camstation/internal/opslog"
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
	want := []liveWarmSpec{{CameraID: 1, StreamName: "소방서4-live", Input: "rtsp://127.0.0.1:8554/%EC%86%8C%EB%B0%A9%EC%84%9C4-live"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("specs = %#v, want %#v", got, want)
	}
}

func TestBuildLiveWarmFFmpegArgsConsumesVideoWithoutEncoding(t *testing.T) {
	input := "rtsp://127.0.0.1:8554/camera-live"
	got := BuildLiveWarmFFmpegArgs(input)
	want := []string{
		"ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "warning", "-nostats",
		"-stats_period", "60", "-progress", "pipe:1",
		"-fflags", "+genpts", "-use_wallclock_as_timestamps", "1",
		"-rtsp_transport", "tcp", "-user_agent", "CamStationWarm/2.0", "-i", input,
		"-map", "0:v:0", "-c:v", "copy", "-an", "-f", "null", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestLiveWarmManagerStartsAllStreamsConcurrently(t *testing.T) {
	started := make(chan string, 8)
	runner := func(stop <-chan struct{}, spec liveWarmSpec, _ int, setActive func(bool)) error {
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
	runner := func(stop <-chan struct{}, spec liveWarmSpec, _ int, setActive func(bool)) error {
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

func TestLiveWarmWorkerLogsAttemptExitAndRetryWithoutInputURL(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	secondAttempt := make(chan struct{})
	runner := func(stop <-chan struct{}, _ liveWarmSpec, attempt int, setActive func(bool)) error {
		if attempt == 1 {
			return errors.New(`open rtsp://admin:secret@127.0.0.1/live?token=bad: connection refused`)
		}
		close(secondAttempt)
		setActive(true)
		<-stop
		return nil
	}
	manager := newLiveWarmManager(
		withLiveWarmRunner(runner),
		withLiveWarmRetry(5*time.Millisecond, 10*time.Millisecond),
		withLiveWarmLogger(logger),
	)
	t.Cleanup(manager.StopAll)
	manager.Reconcile([]store.Camera{{
		ID: 1, Enabled: true, Outputs: []store.CameraOutput{{Purpose: store.CameraOutputLive, StreamName: "gate-live"}},
	}})
	select {
	case <-secondAttempt:
	case <-time.After(time.Second):
		t.Fatal("worker did not retry")
	}

	logs := decodeOperationalLines(t, output.String())
	for _, event := range []string{"worker_attempt_started", "worker_exited", "retry_scheduled"} {
		if !hasOperationalEvent(logs, event) {
			t.Fatalf("missing event %q in %s", event, output.String())
		}
	}
	lower := strings.ToLower(output.String())
	for _, forbidden := range []string{"admin", "secret", "token=", "rtsp://"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("warm log leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestLiveWarmProgressLogsFirstMediaThenDebugProgress(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	input := strings.NewReader("frame=15\nout_time_us=1000000\nprogress=continue\nframe=900\nout_time_us=60000000\nprogress=continue\n")
	if err := scanLiveWarmProgress(input, logger, liveWarmSpec{CameraID: 7, StreamName: "gate-live"}, 3); err != nil {
		t.Fatal(err)
	}
	logs := decodeOperationalLines(t, output.String())
	if len(logs) != 2 || logs[0].Event != "media_started" || logs[0].Level != "info" ||
		logs[0].CameraID != 7 || logs[0].Frame != 15 || logs[0].MediaTimeMS != 1000 || logs[0].Attempt != 3 ||
		logs[1].Event != "media_progress" || logs[1].Level != "debug" || logs[1].Frame != 900 {
		t.Fatalf("progress logs=%+v", logs)
	}
}

func TestLiveWarmStderrIsBoundedRedactedWarning(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	input := strings.NewReader("[tcp] open rtsp://viewer:pw@10.0.0.2/live?token=bad: Connection timed out\n")
	if err := scanLiveWarmStderr(input, logger, liveWarmSpec{StreamName: "gate-live"}, 2); err != nil {
		t.Fatal(err)
	}
	logs := decodeOperationalLines(t, output.String())
	if len(logs) != 1 || logs[0].Event != "ffmpeg_warning" || logs[0].Level != "warn" || logs[0].Attempt != 2 {
		t.Fatalf("stderr logs=%+v", logs)
	}
	lower := strings.ToLower(output.String())
	for _, forbidden := range []string{"viewer", "pw", "10.0.0.2", "token=", "rtsp://"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("stderr log leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestLiveWarmRepeatedStderrIsRateLimited(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	var input strings.Builder
	for index := 0; index < 1000; index++ {
		input.WriteString("PTS ")
		input.WriteString(strconv.Itoa(index + 10))
		input.WriteString(", next:")
		input.WriteString(strconv.Itoa(index + 20))
		input.WriteString(" invalid dropping st:0\n")
	}
	if err := scanLiveWarmStderr(strings.NewReader(input.String()), logger, liveWarmSpec{CameraID: 7, StreamName: "gate-live"}, 2); err != nil {
		t.Fatal(err)
	}
	logs := decodeOperationalLines(t, output.String())
	if len(logs) != 1 || logs[0].Event != "ffmpeg_error" || logs[0].MessageFingerprint == "" {
		t.Fatalf("stderr logs=%+v", logs)
	}
}

func decodeOperationalLines(t *testing.T, value string) []opslog.Record {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(value), "\n")
	result := make([]opslog.Record, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var record opslog.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log %q: %v", line, err)
		}
		result = append(result, record)
	}
	return result
}

func hasOperationalEvent(records []opslog.Record, event string) bool {
	for _, record := range records {
		if record.Event == event {
			return true
		}
	}
	return false
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
