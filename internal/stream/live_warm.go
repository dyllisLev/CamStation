package stream

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"camstation/internal/opslog"
	"camstation/internal/store"
)

const (
	liveWarmRetryMin    = time.Second
	liveWarmRetryMax    = 15 * time.Second
	liveWarmStableAfter = 30 * time.Second
	liveWarmStopTimeout = 5 * time.Second
)

type liveWarmSpec struct {
	CameraID   int64
	StreamName string
	Input      string
}

type liveWarmRunner func(stop <-chan struct{}, spec liveWarmSpec, attempt int, setActive func(bool)) error

type liveWarmOption func(*LiveWarmManager)

type LiveWarmManager struct {
	mu       sync.Mutex
	workers  map[string]*liveWarmWorker
	rtspBase string
	runner   liveWarmRunner
	logger   *opslog.Logger
	retryMin time.Duration
	retryMax time.Duration
}

type liveWarmWorker struct {
	manager *LiveWarmManager
	spec    liveWarmSpec
	stop    chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	active  bool
	lastErr string
}

type LiveWarmSnapshot struct {
	Expected []string
	Active   map[string]bool
}

func newLiveWarmManager(options ...liveWarmOption) *LiveWarmManager {
	manager := &LiveWarmManager{
		workers:  make(map[string]*liveWarmWorker),
		rtspBase: "rtsp://127.0.0.1:8554",
		retryMin: liveWarmRetryMin,
		retryMax: liveWarmRetryMax,
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	if manager.runner == nil {
		manager.runner = manager.runLiveWarmFFmpeg
	}
	return manager
}

func withLiveWarmRunner(runner liveWarmRunner) liveWarmOption {
	return func(manager *LiveWarmManager) {
		if runner != nil {
			manager.runner = runner
		}
	}
}

func withLiveWarmLogger(logger *opslog.Logger) liveWarmOption {
	return func(manager *LiveWarmManager) {
		manager.logger = logger
	}
}

func withLiveWarmRetry(minimum, maximum time.Duration) liveWarmOption {
	return func(manager *LiveWarmManager) {
		if minimum > 0 {
			manager.retryMin = minimum
		}
		if maximum >= manager.retryMin {
			manager.retryMax = maximum
		}
	}
}

func (m *LiveWarmManager) Reconcile(cameras []store.Camera) {
	specs := liveWarmSpecs(cameras, m.rtspBase)
	wanted := make(map[string]liveWarmSpec, len(specs))
	for _, spec := range specs {
		wanted[spec.StreamName] = spec
	}

	m.mu.Lock()
	stale := make([]*liveWarmWorker, 0)
	for name, worker := range m.workers {
		spec, ok := wanted[name]
		if ok && spec == worker.spec {
			continue
		}
		delete(m.workers, name)
		stale = append(stale, worker)
	}
	m.mu.Unlock()

	for _, worker := range stale {
		worker.stopWorker()
	}

	m.mu.Lock()
	started := make([]*liveWarmWorker, 0)
	for _, spec := range specs {
		if _, ok := m.workers[spec.StreamName]; ok {
			continue
		}
		worker := &liveWarmWorker{
			manager: m,
			spec:    spec,
			stop:    make(chan struct{}),
			done:    make(chan struct{}),
		}
		m.workers[spec.StreamName] = worker
		started = append(started, worker)
	}
	m.mu.Unlock()

	for _, worker := range started {
		go worker.run()
	}
}

func (m *LiveWarmManager) StopAll() {
	m.mu.Lock()
	workers := make([]*liveWarmWorker, 0, len(m.workers))
	for name, worker := range m.workers {
		delete(m.workers, name)
		workers = append(workers, worker)
	}
	m.mu.Unlock()
	for _, worker := range workers {
		worker.stopWorker()
	}
}

func (m *LiveWarmManager) Snapshot() LiveWarmSnapshot {
	m.mu.Lock()
	workers := make([]*liveWarmWorker, 0, len(m.workers))
	for _, worker := range m.workers {
		workers = append(workers, worker)
	}
	m.mu.Unlock()

	snapshot := LiveWarmSnapshot{
		Expected: make([]string, 0, len(workers)),
		Active:   make(map[string]bool, len(workers)),
	}
	for _, worker := range workers {
		snapshot.Expected = append(snapshot.Expected, worker.spec.StreamName)
		if worker.isActive() {
			snapshot.Active[worker.spec.StreamName] = true
		}
	}
	sort.Strings(snapshot.Expected)
	return snapshot
}

func liveWarmSpecs(cameras []store.Camera, rtspBase string) []liveWarmSpec {
	base := strings.TrimRight(rtspBase, "/")
	seen := make(map[string]bool, len(cameras))
	specs := make([]liveWarmSpec, 0, len(cameras))
	for _, camera := range cameras {
		if !camera.Enabled {
			continue
		}
		for _, output := range camera.Outputs {
			name := strings.TrimSpace(output.StreamName)
			if output.Purpose != store.CameraOutputLive || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			specs = append(specs, liveWarmSpec{
				CameraID:   camera.ID,
				StreamName: name,
				Input:      base + "/" + url.PathEscape(name),
			})
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].StreamName < specs[j].StreamName })
	return specs
}

func BuildLiveWarmFFmpegArgs(input string) []string {
	return []string{
		"ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "warning", "-nostats",
		"-stats_period", "60", "-progress", "pipe:1",
		"-rtsp_transport", "tcp", "-user_agent", "CamStationWarm/2.0", "-i", input,
		"-map", "0:v:0", "-c:v", "copy", "-an", "-f", "null", "-",
	}
}

func (w *liveWarmWorker) run() {
	defer close(w.done)
	delay := w.manager.retryMin
	attempt := 0
	for {
		select {
		case <-w.stop:
			w.setActive(false)
			return
		default:
		}

		attempt++
		startedAt := time.Now()
		w.manager.log(opslog.Info, "worker_attempt_started", opslog.Fields{
			CameraID: w.spec.CameraID, StreamName: w.spec.StreamName, Attempt: attempt,
		})
		err := w.manager.runner(w.stop, w.spec, attempt, w.setActive)
		w.setActive(false)
		if err != nil {
			safeError := opslog.SanitizeMessage(err.Error())
			w.setError(safeError)
			w.manager.log(opslog.Warn, "worker_exited", opslog.Fields{
				CameraID: w.spec.CameraID, StreamName: w.spec.StreamName, Attempt: attempt,
				DurationMS: time.Since(startedAt).Milliseconds(), ErrorCode: liveWarmErrorCode(err), Message: safeError,
			})
		}

		select {
		case <-w.stop:
			w.manager.log(opslog.Info, "worker_stopped", opslog.Fields{
				CameraID: w.spec.CameraID, StreamName: w.spec.StreamName, Attempt: attempt,
				DurationMS: time.Since(startedAt).Milliseconds(),
			})
			return
		default:
		}
		if time.Since(startedAt) >= liveWarmStableAfter {
			delay = w.manager.retryMin
		}
		w.manager.log(opslog.Warn, "retry_scheduled", opslog.Fields{
			CameraID: w.spec.CameraID, StreamName: w.spec.StreamName, Attempt: attempt, RetryMS: delay.Milliseconds(),
		})
		timer := time.NewTimer(delay)
		select {
		case <-w.stop:
			if !timer.Stop() {
				<-timer.C
			}
			w.manager.log(opslog.Info, "worker_stopped", opslog.Fields{
				CameraID: w.spec.CameraID, StreamName: w.spec.StreamName, Attempt: attempt,
				DurationMS: time.Since(startedAt).Milliseconds(),
			})
			return
		case <-timer.C:
		}
		if delay < w.manager.retryMax {
			delay *= 2
			if delay > w.manager.retryMax {
				delay = w.manager.retryMax
			}
		}
	}
}

func (m *LiveWarmManager) log(level opslog.Level, event string, fields opslog.Fields) {
	if m != nil && m.logger != nil {
		_ = m.logger.Log(level, "stream.live_warm", event, fields)
	}
}

func (w *liveWarmWorker) stopWorker() {
	close(w.stop)
	<-w.done
}

func (w *liveWarmWorker) setActive(active bool) {
	w.mu.Lock()
	w.active = active
	if active {
		w.lastErr = ""
	}
	w.mu.Unlock()
}

func (w *liveWarmWorker) setError(message string) {
	w.mu.Lock()
	w.lastErr = message
	w.mu.Unlock()
}

func (w *liveWarmWorker) isActive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

func runLiveWarmFFmpeg(stop <-chan struct{}, spec liveWarmSpec, setActive func(bool)) error {
	return runLiveWarmFFmpegObserved(stop, spec, 1, setActive, nil)
}

func (m *LiveWarmManager) runLiveWarmFFmpeg(stop <-chan struct{}, spec liveWarmSpec, attempt int, setActive func(bool)) error {
	return runLiveWarmFFmpegObserved(stop, spec, attempt, setActive, m.logger)
}

func runLiveWarmFFmpegObserved(stop <-chan struct{}, spec liveWarmSpec, attempt int, setActive func(bool), logger *opslog.Logger) error {
	args := BuildLiveWarmFFmpegArgs(spec.Input)
	cmd := exec.Command(args[0], args[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg progress pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	setActive(true)
	if logger != nil {
		_ = logger.Log(opslog.Debug, "stream.live_warm", "process_started", opslog.Fields{
			CameraID: spec.CameraID, StreamName: spec.StreamName, Attempt: attempt,
		})
	}
	scanDone := make(chan error, 2)
	go func() { scanDone <- scanLiveWarmProgress(stdout, logger, spec, attempt) }()
	go func() { scanDone <- scanLiveWarmStderr(stderr, logger, spec, attempt) }()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case <-stop:
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		timer := time.NewTimer(liveWarmStopTimeout)
		defer timer.Stop()
		select {
		case <-waitDone:
			<-scanDone
			<-scanDone
			return nil
		case <-timer.C:
			if cmd.Process != nil && cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
			<-waitDone
			<-scanDone
			<-scanDone
			return nil
		}
	case err := <-waitDone:
		progressErr := <-scanDone
		stderrErr := <-scanDone
		if err == nil {
			return errors.Join(errors.New("live warm ffmpeg stopped"), progressErr, stderrErr)
		}
		return errors.Join(errors.New("live warm ffmpeg failed"), progressErr, stderrErr)
	}
}

func scanLiveWarmProgress(reader io.Reader, logger *opslog.Logger, spec liveWarmSpec, attempt int) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), maxBufferedLogLine)
	var frame, mediaTimeUS int64
	started := false
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "frame":
			frame, _ = strconv.ParseInt(value, 10, 64)
		case "out_time_us":
			mediaTimeUS, _ = strconv.ParseInt(value, 10, 64)
		case "progress":
			if logger == nil || (frame <= 0 && mediaTimeUS <= 0) {
				continue
			}
			event, level := "media_progress", opslog.Debug
			if !started {
				started = true
				event, level = "media_started", opslog.Info
			}
			_ = logger.Log(level, "stream.live_warm", event, opslog.Fields{
				CameraID: spec.CameraID, StreamName: spec.StreamName, Attempt: attempt,
				Frame: frame, MediaTimeMS: mediaTimeUS / 1000,
			})
		}
	}
	return scanner.Err()
}

func scanLiveWarmStderr(reader io.Reader, logger *opslog.Logger, spec liveWarmSpec, attempt int) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), maxBufferedLogLine)
	for scanner.Scan() {
		message := strings.TrimSpace(scanner.Text())
		if logger == nil || message == "" {
			continue
		}
		level := opslog.Warn
		event := "ffmpeg_warning"
		lower := strings.ToLower(message)
		if strings.Contains(lower, "error") || strings.Contains(lower, "invalid") || strings.Contains(lower, "failed") {
			level = opslog.Error
			event = "ffmpeg_error"
		}
		_ = logger.Log(level, "stream.live_warm", event, opslog.Fields{
			CameraID: spec.CameraID, StreamName: spec.StreamName, Attempt: attempt,
			ErrorCode: event, Message: message,
		})
	}
	return scanner.Err()
}

func liveWarmErrorCode(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "start ffmpeg"):
		return "process_start_failed"
	case strings.Contains(message, "progress pipe") || strings.Contains(message, "stderr pipe"):
		return "process_pipe_failed"
	default:
		return "process_exited"
	}
}

func liveReadiness(snapshot LiveWarmSnapshot, runtime map[string]StreamRuntime) (expected, ready int, mediaReady bool) {
	expected = len(snapshot.Expected)
	for _, name := range snapshot.Expected {
		item := runtime[name]
		if snapshot.Active[name] && item.ProducerCount > 0 && item.ConsumerCount > 0 {
			ready++
		}
	}
	return expected, ready, ready == expected
}
