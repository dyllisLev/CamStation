package recorder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	afterSegment   func()
	diskGuard      diskGuard

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
	camera          store.Camera
	input           string
	audioMode       store.CameraAudioMode
	appliedRevision int64
	manager         *Manager

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

func New(db *store.DB, recordingsDir, tempDir string, segmentMinutes int, opts ...Option) *Manager {
	if recordingsDir == "" {
		recordingsDir = "./data/recordings"
	}
	if tempDir == "" {
		tempDir = "./data/temp"
	}
	if segmentMinutes <= 0 {
		segmentMinutes = 30
	}
	manager := &Manager{
		db:             db,
		recordingsDir:  recordingsDir,
		tempDir:        tempDir,
		segmentMinutes: segmentMinutes,
		rtspBase:       "rtsp://127.0.0.1:8554",
		diskGuard:      defaultDiskGuard(),
		workers:        map[string]*worker{},
	}
	for _, opt := range opts {
		opt(manager)
	}
	return manager
}

func (m *Manager) Reconcile(cameras []store.Camera) {
	wanted := map[string]store.Camera{}
	for _, camera := range cameras {
		camera = recordingCamera(camera)
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
	spec, err := recordingSpec(camera, m.rtspBase)
	if err != nil {
		return err
	}
	m.startSpec(spec)
	return nil
}

func (m *Manager) startSpec(spec recordSpec) {
	m.mu.Lock()
	if existing, ok := m.workers[spec.camera.StreamName]; ok {
		if existing.audioMode == spec.audioMode && existing.appliedRevision == spec.camera.PolicyState.AppliedRevision {
			m.mu.Unlock()
			return
		}
		delete(m.workers, spec.camera.StreamName)
		m.mu.Unlock()
		existing.stopWorker()
		m.mu.Lock()
	}
	w := &worker{
		camera:          spec.camera,
		input:           spec.input,
		audioMode:       spec.audioMode,
		appliedRevision: spec.camera.PolicyState.AppliedRevision,
		manager:         m,
		state:           statusRunning,
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
	m.workers[spec.camera.StreamName] = w
	m.mu.Unlock()
	go w.run()
}

func recordingCamera(camera store.Camera) store.Camera {
	for _, output := range camera.Outputs {
		if output.Purpose == store.CameraOutputRecording && output.AppliedPolicy.SourceKey != "" {
			camera.StreamName = output.StreamName
			return camera
		}
	}
	if camera.RecordingStreamName != "" {
		camera.StreamName = camera.RecordingStreamName
	}
	return camera
}

type recordSpec struct {
	camera    store.Camera
	input     string
	audioMode store.CameraAudioMode
}

func recordingSpec(camera store.Camera, rtspBase string) (recordSpec, error) {
	audioMode := store.CameraAudioSource
	for _, output := range camera.Outputs {
		if output.Purpose == store.CameraOutputRecording && output.AppliedPolicy.SourceKey != "" {
			camera.StreamName = output.StreamName
			audioMode = output.AppliedPolicy.AudioMode
			break
		}
	}
	camera = recordingCamera(camera)
	if camera.StreamName == "" {
		return recordSpec{}, fmt.Errorf("camera recording stream name is required")
	}
	return recordSpec{
		camera:    camera,
		input:     strings.TrimRight(rtspBase, "/") + "/" + camera.StreamName,
		audioMode: audioMode,
	}, nil
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
	_ = m.SuspendActive()
}

func (m *Manager) SuspendActive() []store.Camera {
	m.mu.Lock()
	workers := make([]*worker, 0, len(m.workers))
	cameras := make([]store.Camera, 0, len(m.workers))
	for streamName, worker := range m.workers {
		delete(m.workers, streamName)
		workers = append(workers, worker)
		cameras = append(cameras, worker.camera)
	}
	m.mu.Unlock()
	for _, worker := range workers {
		worker.stopWorker()
	}
	return cameras
}

func (m *Manager) RestoreActive(cameras []store.Camera) error {
	specs := make([]recordSpec, 0, len(cameras))
	for _, camera := range cameras {
		spec, err := recordingSpec(camera, m.rtspBase)
		if err != nil {
			return err
		}
		specs = append(specs, spec)
	}
	for _, spec := range specs {
		m.startSpec(spec)
	}
	return nil
}

func (m *Manager) SetAfterSegmentClosed(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.afterSegment = fn
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

func (m *Manager) notifySegmentClosed() {
	m.mu.Lock()
	fn := m.afterSegment
	m.mu.Unlock()
	if fn != nil {
		go fn()
	}
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
		if errors.Is(err, ErrRecordingDiskFull) {
			w.setState(statusPaused, "", err.Error())
			log.Printf("recorder %s paused: %v", w.camera.StreamName, err)
		} else if err != nil {
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
	archiveName := RecordingArchiveName(w.camera.Name, w.camera.StreamName)
	outputDir := filepath.Join(w.manager.tempDir, archiveName, now.Format("2006-01-02"))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(w.manager.recordingsDir, 0o755); err != nil {
		return err
	}
	if err := w.manager.checkDiskCapacity(); err != nil {
		return err
	}

	cmdArgs := BuildFFmpegArgsForPolicy(w.input, outputDir, w.manager.segmentMinutes, archiveName, w.audioMode)
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
	w.setState(statusRunning, outputDir, "")
	log.Printf("recorder started stream=%s pid=%d input=%s output=%s", w.camera.StreamName, cmd.Process.Pid, w.input, outputDir)

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		w.watchStderr(stderr)
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	if err = w.waitForProcess(cmd, waitDone); err != nil {
		if errors.Is(err, ErrRecordingDiskFull) {
			w.setState(statusPaused, "", err.Error())
		}
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
	finalPath, size, err := MoveToRecordings(segment.path, w.camera.Name, w.camera.StreamName, w.manager.recordingsDir)
	if err != nil {
		_ = w.manager.db.MarkRecordingSegmentStatus(context.Background(), w.camera.StreamName, segment.filename, "failed", err.Error())
		w.setState(statusRunning, segment.path, err.Error())
		return
	}
	if err := w.manager.db.CloseRecordingSegment(context.Background(), w.camera.StreamName, segment.filename, tsEnd, finalPath, size); err != nil {
		w.setState(statusRunning, segment.path, err.Error())
		return
	}
	w.manager.notifySegmentClosed()
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
	return BuildFFmpegArgsForCamera(input, outputDir, segmentMinutes, "")
}

func BuildFFmpegArgsForCamera(input, outputDir string, segmentMinutes int, archiveName string) []string {
	return BuildFFmpegArgsForPolicy(input, outputDir, segmentMinutes, archiveName, store.CameraAudioSource)
}

func BuildFFmpegArgsForPolicy(input, outputDir string, segmentMinutes int, archiveName string, audioMode store.CameraAudioMode) []string {
	if segmentMinutes <= 0 {
		segmentMinutes = 30
	}
	filenamePattern := "%Y-%m-%d_%H-%M.mp4"
	if archiveName != "" {
		filenamePattern = archiveName + "_" + filenamePattern
	}
	outputPattern := filepath.Join(outputDir, filenamePattern)
	args := []string{
		"ffmpeg", "-y",
		"-nostats",
		"-fflags", "+genpts",
		"-rtsp_transport", "tcp",
		"-i", input,
		"-c:v", "copy",
	}
	switch audioMode {
	case store.CameraAudioNone:
		args = append(args, "-an")
	case store.CameraAudioAAC:
		args = append(args, "-c:a", "copy")
	default:
		args = append(args, "-af", "asetpts=PTS-STARTPTS", "-c:a", "aac")
	}
	return append(args,
		"-f", "segment",
		"-segment_time", strconv.Itoa(segmentMinutes*60),
		"-segment_atclocktime", "1",
		"-reset_timestamps", "1",
		"-strftime", "1",
		"-avoid_negative_ts", "make_zero",
		outputPattern,
	)
}

func RecordingArchiveName(cameraName, streamName string) string {
	name := sanitizeArchiveName(cameraName)
	if name == "" {
		name = sanitizeArchiveName(streamName)
	}
	if name == "" {
		return "camera"
	}
	return name
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
	matches := regexp.MustCompile(`^(?:.+_)?(\d{4}-\d{2}-\d{2})_(\d{2})-(\d{2})$`).FindStringSubmatch(stem)
	if len(matches) != 4 {
		return 0, false
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", matches[1]+" "+matches[2]+":"+matches[3], kst())
	if err != nil {
		return 0, false
	}
	return float64(parsed.Unix()), true
}

func MoveToRecordings(tempPath, cameraName, streamName, recordingsDir string) (string, *int64, error) {
	date, ok := dateFromSegmentPath(tempPath)
	if !ok {
		return "", nil, fmt.Errorf("cannot determine segment date from %s", tempPath)
	}
	archiveName := RecordingArchiveName(cameraName, streamName)
	finalDir := filepath.Join(recordingsDir, archiveName, date)
	finalPath := filepath.Join(finalDir, archiveSegmentFilename(tempPath, archiveName))
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
	matches := regexp.MustCompile(`^(?:.+_)?(\d{4}-\d{2}-\d{2})_\d{2}-\d{2}$`).FindStringSubmatch(stem)
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

func archiveSegmentFilename(path, archiveName string) string {
	stem := fileStem(path)
	matches := regexp.MustCompile(`^(?:.+_)?(\d{4}-\d{2}-\d{2}_\d{2}-\d{2})$`).FindStringSubmatch(stem)
	if len(matches) != 2 {
		return filepath.Base(path)
	}
	return archiveName + "_" + matches[1] + filepath.Ext(path)
}

func sanitizeArchiveName(name string) string {
	value := strings.TrimSpace(name)
	value = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]+`).ReplaceAllString(value, "-")
	value = regexp.MustCompile(`\s+`).ReplaceAllString(value, "-")
	value = regexp.MustCompile(`-+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-. ")
	return value
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
