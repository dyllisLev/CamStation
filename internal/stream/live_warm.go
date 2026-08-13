package stream

import (
	"errors"
	"io"
	"log"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"camstation/internal/store"
)

const (
	liveWarmRetryMin    = time.Second
	liveWarmRetryMax    = 15 * time.Second
	liveWarmStableAfter = 30 * time.Second
	liveWarmStopTimeout = 5 * time.Second
)

type liveWarmSpec struct {
	StreamName string
	Input      string
}

type liveWarmRunner func(stop <-chan struct{}, spec liveWarmSpec, setActive func(bool)) error

type liveWarmOption func(*LiveWarmManager)

type LiveWarmManager struct {
	mu       sync.Mutex
	workers  map[string]*liveWarmWorker
	rtspBase string
	runner   liveWarmRunner
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
		runner:   runLiveWarmFFmpeg,
		retryMin: liveWarmRetryMin,
		retryMax: liveWarmRetryMax,
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
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
		"ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-nostats",
		"-rtsp_transport", "tcp", "-user_agent", "CamStationWarm/2.0", "-i", input,
		"-map", "0:v:0", "-c:v", "copy", "-an", "-f", "null", "-",
	}
}

func (w *liveWarmWorker) run() {
	defer close(w.done)
	delay := w.manager.retryMin
	for {
		select {
		case <-w.stop:
			w.setActive(false)
			return
		default:
		}

		startedAt := time.Now()
		err := w.manager.runner(w.stop, w.spec, w.setActive)
		w.setActive(false)
		if err != nil {
			w.setError("live warm consumer exited")
			log.Printf("live warm consumer exited stream=%s", w.spec.StreamName)
		}

		select {
		case <-w.stop:
			return
		default:
		}
		if time.Since(startedAt) >= liveWarmStableAfter {
			delay = w.manager.retryMin
		}
		timer := time.NewTimer(delay)
		select {
		case <-w.stop:
			if !timer.Stop() {
				<-timer.C
			}
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
	args := BuildLiveWarmFFmpegArgs(spec.Input)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	setActive(true)
	log.Printf("live warm consumer started stream=%s pid=%d", spec.StreamName, cmd.Process.Pid)

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
			return nil
		case <-timer.C:
			if cmd.Process != nil && cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
			}
			<-waitDone
			return nil
		}
	case err := <-waitDone:
		if err == nil {
			return errors.New("live warm consumer stopped")
		}
		return errors.New("live warm consumer failed")
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
