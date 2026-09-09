// Package ffmpegsupervisor bounds NVENC use across go2rtc child processes.
package ffmpegsupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultStateDir = "/tmp/camstation-nvenc"

var errCapacity = errors.New("NVENC capacity exhausted")

type Config struct {
	Binary, StateDir string
	ProbeTimeout     time.Duration
	slot             *os.File
	onStart          func()
	onGPUFailure     func(string)
	Stdin            io.Reader
	Stdout, Stderr   io.Writer
}

type Status struct {
	Output           string    `json:"output"`
	RequestedEncoder string    `json:"requestedEncoder"`
	ActualEncoder    string    `json:"actualEncoder"`
	FallbackReason   string    `json:"fallbackReason,omitempty"`
	Running          bool      `json:"running"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type record struct {
	Status
	PID           int    `json:"pid"`
	StartIdentity string `json:"startIdentity"`
}

var outputPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// Run returns the underlying exit code. Only explicitly selected h264_nvenc
// outputs use admission control or the synthetic capability check.
func Run(ctx context.Context, cfg Config, args []string) int {
	if cfg.Binary == "" {
		cfg.Binary = "ffmpeg"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultStateDir
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 8 * time.Second
	}
	clean := make([]string, 0, len(args))
	output := ""
	requested, reason := "", ""
	for i := 0; i < len(args); i++ {
		if args[i] == "-camstation-requested" || args[i] == "-camstation-reason" {
			if i+1 >= len(args) {
				return 2
			}
			if args[i] == "-camstation-requested" {
				if args[i+1] != "cpu" && args[i+1] != "nvenc" {
					return 2
				}
				requested = args[i+1]
			} else {
				if args[i+1] != "session_limit" {
					return 2
				}
				reason = args[i+1]
			}
			i++
			continue
		}
		if args[i] == "-camstation-output" {
			if i+1 >= len(args) || !outputPattern.MatchString(args[i+1]) {
				return 2
			}
			output = args[i+1]
			i++
			continue
		}
		clean = append(clean, args[i])
	}
	nvenc := false
	for i := 0; i+1 < len(clean); i++ {
		if isVideoCodec(clean[i]) && clean[i+1] == "h264_nvenc" {
			nvenc = true
		}
	}
	status := Status{Output: output, RequestedEncoder: "cpu", ActualEncoder: "cpu"}
	if nvenc {
		status.RequestedEncoder = "nvenc"
	}
	if requested != "" {
		status.RequestedEncoder = requested
	}
	status.FallbackReason = reason
	publish := func(running bool) {
		status.Running = running
		status.UpdatedAt = time.Now().UTC()
		if output != "" {
			writeStatus(cfg.StateDir, status)
		}
	}
	parent := currentParent()
	latchedReason := ""
	if nvenc && output != "" {
		latchedReason = readFallback(cfg.StateDir, output, parent)
	}
	if latchedReason != "" {
		status.FallbackReason = latchedReason
	}
	var release func()
	if nvenc && latchedReason == "" {
		var err error
		cfg.slot, err = acquireSlot(cfg.StateDir)
		if err == nil {
			release = func() { _ = cfg.slot.Close() }
		}
		if err != nil {
			status.FallbackReason = "encoder_failed"
			if errors.Is(err, errCapacity) {
				status.FallbackReason = "session_limit"
			} else if errors.Is(err, os.ErrPermission) {
				status.FallbackReason = "permission_denied"
			}
		} else {
			probeCtx, cancel := context.WithTimeout(ctx, cfg.ProbeTimeout)
			probe := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=1280x720:r=30", "-frames:v", "3", "-an", "-c:v", "h264_nvenc", "-preset:v", "llhp", "-tune:v", "ll", "-pix_fmt:v", "yuv420p", "-g", "20", "-bf", "0", "-zerolatency", "1", "-f", "null", "-"}
			code, detail := execute(probeCtx, cfg, probe, true, true)
			timedOut := probeCtx.Err() == context.DeadlineExceeded
			cancel()
			if ctx.Err() != nil {
				release()
				publish(false)
				return 130
			}
			if code != 0 {
				status.FallbackReason = failureReason(detail)
				if timedOut {
					status.FallbackReason = "probe_timeout"
				}
				if output != "" {
					writeFallback(cfg.StateDir, output, parent, status.FallbackReason)
				}
				release()
				release = nil
				cfg.slot = nil
			} else {
				status.ActualEncoder = "nvenc"
			}
		}
	}
	if nvenc && status.ActualEncoder != "nvenc" {
		clean = cpuArgs(clean)
	}
	cfg.onStart = func() { publish(true) }
	gpuReason := ""
	if status.ActualEncoder == "nvenc" {
		cfg.onGPUFailure = func(reason string) {
			gpuReason = reason
			if output != "" {
				writeFallback(cfg.StateDir, output, parent, reason)
			}
		}
	}
	code, _ := execute(ctx, cfg, clean, false, nvenc || output != "")
	if gpuReason != "" {
		status.FallbackReason = gpuReason
	}
	if release != nil {
		release()
		release = nil
		cfg.slot = nil
	}
	if nvenc && status.ActualEncoder == "nvenc" && code != 0 && ctx.Err() == nil && gpuReason != "" {
		status.ActualEncoder = "cpu"
		status.FallbackReason = gpuReason
		cfg.onGPUFailure = nil
		publish(false)
		code, _ = execute(ctx, cfg, cpuArgs(clean), false, true)
	}
	if code != 0 && ctx.Err() == nil && status.ActualEncoder == "cpu" {
		status.FallbackReason = "cpu_failed"
	}
	publish(false)
	return code
}
func isVideoCodec(s string) bool { return s == "-c:v" || s == "-codec:v" || s == "-vcodec" }
func cpuArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if i+1 < len(args) {
			if isVideoCodec(arg) && args[i+1] == "h264_nvenc" {
				out = append(out, arg, "libx264")
				i++
				continue
			}
			if arg == "-preset" || arg == "-preset:v" {
				out = append(out, arg, "veryfast")
				i++
				continue
			}
			if arg == "-tune" || arg == "-tune:v" {
				out = append(out, arg, "zerolatency")
				i++
				continue
			}
			switch arg {
			case "-rc", "-rc:v", "-cq", "-cq:v", "-gpu", "-gpu:v", "-surfaces", "-delay", "-zerolatency", "-spatial-aq", "-temporal-aq", "-rc-lookahead":
				i++
				continue
			}
		}
		out = append(out, arg)
	}
	if len(out) > 0 {
		last := out[len(out)-1]
		out = append(out[:len(out)-1], "-keyint_min", "20", "-sc_threshold", "0", last)
	}
	return out
}

// The tail retains late encoder failures after long-running FFmpeg statistics.
// GPU errors are classified on every write and persisted before RTSP teardown
// can cause go2rtc to kill this wrapper.
type limitedBuffer struct {
	buffer             bytes.Buffer
	onGPUFailure       func(string)
	gpuFailureRecorded bool
}

func (b *limitedBuffer) Len() int       { return b.buffer.Len() }
func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }
func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if !b.gpuFailureRecorded && b.onGPUFailure != nil {
		if reason := gpuFailureReason(string(b.Bytes()) + string(p)); reason != "" {
			b.onGPUFailure(reason)
			b.gpuFailureRecorded = true
		}
	}
	if len(p) >= 8192 {
		b.buffer.Reset()
		_, _ = b.buffer.Write(p[len(p)-8192:])
	} else {
		if discard := b.Len() + len(p) - 8192; discard > 0 {
			b.buffer.Next(discard)
		}
		_, _ = b.buffer.Write(p)
	}
	return n, nil
}

// Only evidence specific to the GPU encoder may change a live output to CPU.
// RTSP, input, authentication, and filesystem failures are not GPU failures.
func gpuFailureReason(detail string) string {
	s := strings.ToLower(detail)
	for _, line := range strings.Split(s, "\n") {
		gpuContext := strings.Contains(line, "nvenc") || strings.Contains(line, "cuda") || strings.Contains(line, "nvidia") || strings.Contains(line, "nv_enc_")
		switch {
		case gpuContext && strings.Contains(line, "permission denied"):
			return "permission_denied"
		case strings.Contains(line, "required nvenc api"), strings.Contains(line, "minimum required nvidia driver"), strings.Contains(line, "driver does not support the required nvenc"):
			return "api_incompatible"
		case (strings.Contains(line, "cannot load") || strings.Contains(line, "not found") || strings.Contains(line, "cannot open shared object")) && (strings.Contains(line, "libcuda") || strings.Contains(line, "libnvidia-encode") || strings.Contains(line, "nvcuda.dll") || strings.Contains(line, "nvencodeapi")):
			return "library_unavailable"
		case strings.Contains(line, "unknown encoder") && strings.Contains(line, "h264_nvenc"):
			return "library_unavailable"
		case strings.Contains(line, "no capable devices"), strings.Contains(line, "no cuda-capable"), strings.Contains(line, "cuda_error_no_device"):
			return "device_unavailable"
		case strings.Contains(line, "nv_enc_err_"), strings.Contains(line, "cuda_error_"), strings.Contains(line, "openencodesessionex failed"), strings.Contains(line, "initializeencoder failed"), gpuContext && strings.Contains(line, "encodepicture failed"):
			return "encoder_failed"
		}
	}
	return ""
}
func failureReason(detail string) string {
	if reason := gpuFailureReason(detail); reason != "" {
		return reason
	}
	return "encoder_failed"
}
func execute(ctx context.Context, cfg Config, args []string, probe, capture bool) (int, string) {
	cmd := exec.CommandContext(ctx, cfg.Binary, args...)
	configureChild(cmd)
	if cfg.slot != nil {
		cmd.ExtraFiles = []*os.File{cfg.slot}
	}
	cmd.WaitDelay = 2 * time.Second
	var stderr limitedBuffer
	if !probe {
		stderr.onGPUFailure = cfg.onGPUFailure
	}
	if !probe {
		cmd.Stdin = cfg.Stdin
		cmd.Stdout = cfg.Stdout
	}
	if capture || probe {
		cmd.Stderr = &stderr
	} else {
		cmd.Stderr = cfg.Stderr
	}
	err := cmd.Start()
	if err == nil {
		if !probe && cfg.onStart != nil {
			cfg.onStart()
		}
		err = cmd.Wait()
	}
	detail := stderr.String()
	if err == nil {
		return 0, detail
	}
	if ctx.Err() != nil {
		return 130, detail
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() > 0 {
			return exit.ExitCode(), detail
		}
		return 1, detail
	}
	return 127, detail
}
func writeStatus(dir string, s Status) {
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	if !s.Running {
		b, err := os.ReadFile(filepath.Join(dir, s.Output+".json"))
		if err == nil {
			var existing record
			if json.Unmarshal(b, &existing) == nil && existing.PID != os.Getpid() {
				return
			}
		}
	}
	b, err := json.Marshal(record{Status: s, PID: os.Getpid(), StartIdentity: processIdentity(os.Getpid())})
	if err != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".status-")
	if err != nil {
		return
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return
	}
	if f.Close() != nil {
		return
	}
	_ = os.Rename(name, filepath.Join(dir, s.Output+".json"))
}

// ReadStatuses exposes only stable metadata, never command lines or stderr.
// Records belonging to dead wrappers are reported stopped after a crash.
func ReadStatuses(dir string) ([]Status, error) {
	if dir == "" {
		dir = DefaultStateDir
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Status{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Status{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var r record
		if json.Unmarshal(b, &r) != nil || !outputPattern.MatchString(r.Output) {
			continue
		}
		if r.Running && (!processAlive(r.PID) || r.StartIdentity == "" || r.StartIdentity != processIdentity(r.PID)) {
			r.Running = false
		}
		out = append(out, r.Status)
	}
	return out, nil
}
func slotName(dir string, i int) string { return filepath.Join(dir, "slot-"+strconv.Itoa(i)+".lock") }

func processIdentity(pid int) string {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return ""
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}
