package recorder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"camstation/internal/store"
)

const (
	statusRunning = "running"
	statusStopped = "stopped"
)

type Manager struct {
	db             *store.DB
	recordingsDir  string
	tempDir        string
	segmentMinutes int
	rtspBase       string

	mu      sync.Mutex
	workers map[string]*worker
}

type Status struct {
	Enabled        bool           `json:"enabled"`
	RecordingsDir  string         `json:"recordingsDir"`
	TempDir        string         `json:"tempDir"`
	SegmentMinutes int            `json:"segmentMinutes"`
	Workers        []WorkerStatus `json:"workers"`
}

type WorkerStatus struct {
	StreamName string `json:"streamName"`
	CameraID   int64  `json:"camera_id"`
	State      string `json:"state"`
	Input      string `json:"input"`
	Current    string `json:"current,omitempty"`
	LastError  string `json:"lastError,omitempty"`
}

type worker struct {
	camera  store.Camera
	input   string
	manager *Manager

	mu             sync.Mutex
	state          string
	current        string
	lastErr        string
	stop           chan struct{}
	done           chan struct{}
	proc           *exec.Cmd
	currentSegment *segmentRef
}

type segmentRef struct {
	path     string
	filename string
	tsStart  float64
}

func New(db *store.DB, recordingsDir, tempDir string, segmentMinutes int) *Manager {
	if recordingsDir == "" {
		recordingsDir = "./data/recordings"
	}
	if tempDir == "" {
		tempDir = "./data/temp"
	}
	if segmentMinutes <= 0 {
		segmentMinutes = 30
	}
	return &Manager{
		db:             db,
		recordingsDir:  recordingsDir,
		tempDir:        tempDir,
		segmentMinutes: segmentMinutes,
		rtspBase:       "rtsp://127.0.0.1:8554",
		workers:        map[string]*worker{},
	}
}

func (m *Manager) Reconcile(cameras []store.Camera) {
	wanted := map[string]store.Camera{}
	for _, camera := range cameras {
		if camera.StreamName == "" {
			continue
		}
		wanted[camera.StreamName] = camera
		if err := m.Start(camera); err != nil {
			log.Printf("recorder start %s: %v", camera.StreamName, err)
		}
	}

	m.mu.Lock()
	var stale []*worker
	for streamName, worker := range m.workers {
		if _, ok := wanted[streamName]; !ok {
			delete(m.workers, streamName)
			stale = append(stale, worker)
		}
	}
	m.mu.Unlock()

	for _, worker := range stale {
		worker.stopWorker()
	}
}

func (m *Manager) Start(camera store.Camera) error {
	if camera.StreamName == "" {
		return fmt.Errorf("camera stream name is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workers[camera.StreamName]; ok {
		return nil
	}
	input := fmt.Sprintf("%s/%s", m.rtspBase, camera.StreamName)
	w := &worker{
		camera:  camera,
		input:   input,
		manager: m,
		state:   statusRunning,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	m.workers[camera.StreamName] = w
	go w.run()
	return nil
}

func (m *Manager) Stop(streamName string) {
	m.mu.Lock()
	w := m.workers[streamName]
	delete(m.workers, streamName)
	m.mu.Unlock()
	if w != nil {
		w.stopWorker()
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	workers := make([]*worker, 0, len(m.workers))
	for streamName, worker := range m.workers {
		delete(m.workers, streamName)
		workers = append(workers, worker)
	}
	m.mu.Unlock()
	for _, worker := range workers {
		worker.stopWorker()
	}
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	workers := make([]*worker, 0, len(m.workers))
	for _, worker := range m.workers {
		workers = append(workers, worker)
	}
	m.mu.Unlock()

	status := Status{
		Enabled:        len(workers) > 0,
		RecordingsDir:  m.recordingsDir,
		TempDir:        m.tempDir,
		SegmentMinutes: m.segmentMinutes,
		Workers:        make([]WorkerStatus, 0, len(workers)),
	}
	for _, worker := range workers {
		status.Workers = append(status.Workers, worker.status())
	}
	return status
}

func (w *worker) run() {
	defer close(w.done)
	delay := 5 * time.Second
	for {
		select {
		case <-w.stop:
			w.setState(statusStopped, "", "")
			return
		default:
		}

		err := w.runOnce()
		if err != nil {
			w.setState(statusRunning, "", err.Error())
			log.Printf("recorder %s exited: %v", w.camera.StreamName, err)
		}

		select {
		case <-w.stop:
			w.setState(statusStopped, "", "")
			return
		case <-time.After(delay):
		}
		if delay < time.Minute {
			delay *= 2
		}
	}
}

func (w *worker) runOnce() error {
	now := time.Now().In(kst())
	outputDir := filepath.Join(w.manager.tempDir, w.camera.StreamName, now.Format("2006-01-02"))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(w.manager.recordingsDir, 0o755); err != nil {
		return err
	}

	cmdArgs := BuildFFmpegArgs(w.input, outputDir, w.manager.segmentMinutes)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = append(os.Environ(), "TZ=Asia/Seoul")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	w.mu.Lock()
	w.proc = cmd
	w.mu.Unlock()
	log.Printf("recorder started stream=%s pid=%d input=%s output=%s", w.camera.StreamName, cmd.Process.Pid, w.input, outputDir)

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		w.watchStderr(stderr)
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case <-w.stop:
		terminate(cmd, 10*time.Second)
		<-waitDone
	case err = <-waitDone:
	}
	<-scanDone

	w.mu.Lock()
	if w.proc == cmd {
		w.proc = nil
	}
	w.mu.Unlock()
	w.closeCurrent(time.Now().In(kst()).Unix())
	return err
}

func (w *worker) watchStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		path := ParseSegmentPath(scanner.Text())
		if path == "" {
			continue
		}
		if err := w.openSegment(path); err != nil {
			log.Printf("recorder segment handling failed stream=%s path=%s: %v", w.camera.StreamName, path, err)
			w.setState(statusRunning, path, err.Error())
		}
	}
}

func (w *worker) openSegment(path string) error {
	tsStart, ok := TimestampFromSegmentPath(path)
	if !ok {
		return fmt.Errorf("cannot parse segment timestamp from %s", path)
	}
	if current := w.currentRef(); current != nil {
		w.closeSegment(current, tsStart)
	}

	filename := filepath.Base(path)
	_, err := w.manager.db.OpenRecordingSegment(context.Background(), store.RecordingSegment{
		CameraID:   w.camera.ID,
		StreamName: w.camera.StreamName,
		Filename:   filename,
		TempPath:   path,
		TSStart:    tsStart,
		Status:     "recording",
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.currentSegment = &segmentRef{path: path, filename: filename, tsStart: tsStart}
	w.current = path
	w.lastErr = ""
	w.mu.Unlock()
	return nil
}

func (w *worker) closeCurrent(tsEnd int64) {
	current := w.currentRef()
	if current == nil {
		return
	}
	w.closeSegment(current, float64(tsEnd))
	w.mu.Lock()
	if w.currentSegment == current {
		w.currentSegment = nil
		w.current = ""
	}
	w.mu.Unlock()
}

func (w *worker) closeSegment(segment *segmentRef, tsEnd float64) {
	finalPath, size, err := MoveToRecordings(segment.path, w.camera.StreamName, w.manager.recordingsDir)
	if err != nil {
		_ = w.manager.db.MarkRecordingSegmentStatus(context.Background(), w.camera.StreamName, segment.filename, "failed", err.Error())
		w.setState(statusRunning, segment.path, err.Error())
		return
	}
	if err := w.manager.db.CloseRecordingSegment(context.Background(), w.camera.StreamName, segment.filename, tsEnd, finalPath, size); err != nil {
		w.setState(statusRunning, segment.path, err.Error())
	}
}

func (w *worker) currentRef() *segmentRef {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentSegment
}

func (w *worker) stopWorker() {
	close(w.stop)
	w.mu.Lock()
	cmd := w.proc
	w.mu.Unlock()
	if cmd != nil {
		terminate(cmd, 10*time.Second)
	}
	<-w.done
}

func (w *worker) setState(state, current, lastErr string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = state
	if current != "" {
		w.current = current
	}
	w.lastErr = lastErr
}

func (w *worker) status() WorkerStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WorkerStatus{
		StreamName: w.camera.StreamName,
		CameraID:   w.camera.ID,
		State:      w.state,
		Input:      w.input,
		Current:    w.current,
		LastError:  w.lastErr,
	}
}

func BuildFFmpegArgs(input, outputDir string, segmentMinutes int) []string {
	if segmentMinutes <= 0 {
		segmentMinutes = 30
	}
	outputPattern := filepath.Join(outputDir, "%Y-%m-%d_%H-%M.mp4")
	return []string{
		"ffmpeg", "-y",
		"-nostats",
		"-use_wallclock_as_timestamps", "1",
		"-rtsp_transport", "tcp",
		"-i", input,
		"-c:v", "copy",
		"-c:a", "aac",
		"-f", "segment",
		"-segment_time", strconv.Itoa(segmentMinutes * 60),
		"-segment_atclocktime", "1",
		"-reset_timestamps", "1",
		"-strftime", "1",
		"-avoid_negative_ts", "make_zero",
		outputPattern,
	}
}

func ParseSegmentPath(line string) string {
	matches := regexp.MustCompile(`Opening '(.+?\.mp4)' for writing`).FindStringSubmatch(line)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

func TimestampFromSegmentPath(path string) (float64, bool) {
	stem := fileStem(path)
	matches := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})_(\d{2})-(\d{2})$`).FindStringSubmatch(stem)
	if len(matches) != 4 {
		return 0, false
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", matches[1]+" "+matches[2]+":"+matches[3], kst())
	if err != nil {
		return 0, false
	}
	return float64(parsed.Unix()), true
}

func MoveToRecordings(tempPath, streamName, recordingsDir string) (string, *int64, error) {
	date, ok := dateFromSegmentPath(tempPath)
	if !ok {
		return "", nil, fmt.Errorf("cannot determine segment date from %s", tempPath)
	}
	finalDir := filepath.Join(recordingsDir, streamName, date)
	finalPath := filepath.Join(finalDir, filepath.Base(tempPath))
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return "", nil, err
	}
	if _, err := os.Stat(tempPath); err != nil {
		if _, finalErr := os.Stat(finalPath); finalErr == nil {
			size := fileSize(finalPath)
			return finalPath, size, nil
		}
		return "", nil, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", nil, err
	}
	return finalPath, fileSize(finalPath), nil
}

func dateFromSegmentPath(path string) (string, bool) {
	stem := fileStem(path)
	matches := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})_\d{2}-\d{2}$`).FindStringSubmatch(stem)
	if len(matches) == 2 {
		return matches[1], true
	}
	return "", false
}

func fileStem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}

func fileSize(path string) *int64 {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	size := info.Size()
	return &size
}

func terminate(cmd *exec.Cmd, timeout time.Duration) {
	if cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	process := cmd.Process
	_ = process.Signal(syscall.SIGTERM)
	time.AfterFunc(timeout, func() {
		if cmd.ProcessState == nil {
			_ = process.Kill()
		}
	})
}

func kst() *time.Location {
	location, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return location
}
