package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"camstation/internal/store"
)

type Go2RTC struct {
	binary           string
	configPath       string
	apiURL           string
	webrtcCandidates []string
	liveWarmer       *LiveWarmManager

	mu      sync.Mutex
	cmd     *exec.Cmd
	applyMu sync.Mutex
}

type Go2RTCOption func(*Go2RTC)

type Status struct {
	Installed           bool                     `json:"installed"`
	Running             bool                     `json:"running"`
	MediaReady          bool                     `json:"mediaReady"`
	ExpectedLiveStreams int                      `json:"expectedLiveStreams"`
	ReadyLiveStreams    int                      `json:"readyLiveStreams"`
	APIURL              string                   `json:"apiUrl"`
	Error               string                   `json:"error,omitempty"`
	Streams             map[string]StreamRuntime `json:"streams,omitempty"`
}

type StreamRuntime struct {
	State         string `json:"state"`
	ProducerCount int    `json:"producerCount"`
	ConsumerCount int    `json:"consumerCount"`
	ViewerCount   int    `json:"viewerCount"`
}

func NewGo2RTC(configPath string, options ...Go2RTCOption) *Go2RTC {
	g := &Go2RTC{
		binary:     "go2rtc",
		configPath: configPath,
		apiURL:     "http://127.0.0.1:1984",
		liveWarmer: newLiveWarmManager(),
	}
	for _, option := range options {
		if option != nil {
			option(g)
		}
	}
	return g
}

func WithWebRTCCandidates(candidates []string) Go2RTCOption {
	return func(g *Go2RTC) {
		g.webrtcCandidates = append([]string(nil), candidates...)
	}
}

func ParseWebRTCCandidates(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > 8 {
		return nil, fmt.Errorf("at most 8 WebRTC candidates are allowed")
	}
	seen := make(map[string]bool, len(parts))
	candidates := make([]string, 0, len(parts))
	for index, part := range parts {
		candidate := strings.TrimSpace(part)
		address, err := netip.ParseAddrPort(candidate)
		if err != nil || address.Port() == 0 {
			return nil, fmt.Errorf("invalid WebRTC candidate at position %d", index+1)
		}
		ip := address.Addr().Unmap()
		if !ip.IsGlobalUnicast() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return nil, fmt.Errorf("WebRTC candidate at position %d is not externally reachable", index+1)
		}
		address = netip.AddrPortFrom(ip, address.Port())
		canonical := address.String()
		if !seen[canonical] {
			seen[canonical] = true
			candidates = append(candidates, canonical)
		}
	}
	return candidates, nil
}

func (g *Go2RTC) Ensure(ctx context.Context, cameras []store.Camera) error {
	return g.ensure(ctx, cameras, g.restartProcess)
}

func (g *Go2RTC) ensure(ctx context.Context, cameras []store.Camera, restart func(context.Context) error) error {
	hadDisabled := len(enabledCameras(cameras)) != len(cameras)
	config, preserve, err := g.startupConfig(cameras)
	if err != nil {
		return err
	}
	if preserve {
		if err := writeFileAtomic(g.configPath, config); err != nil {
			return err
		}
		return g.Start(ctx)
	}
	if hadDisabled {
		return g.applyConfigFailClosed(ctx, config, restart)
	}
	return g.applyConfig(ctx, config, restart)
}

func (g *Go2RTC) WriteConfig(cameras []store.Camera) error {
	config, _, err := g.startupConfig(cameras)
	if err != nil {
		return err
	}
	return writeFileAtomic(g.configPath, config)
}

func (g *Go2RTC) startupConfig(cameras []store.Camera) ([]byte, bool, error) {
	hadDisabled := len(enabledCameras(cameras)) != len(cameras)
	cameras = enabledCameras(cameras)
	preserve := false
	hasApplied := false
	for _, camera := range cameras {
		if camera.PolicyState.AppliedRevision > 0 {
			hasApplied = true
			break
		}
	}
	for _, camera := range cameras {
		state := camera.PolicyState
		if state.ApplyState == store.CameraApplyFailed || (state.DesiredRevision != state.AppliedRevision && (state.AppliedRevision > 0 || hasApplied)) {
			preserve = true
			break
		}
	}
	if !preserve {
		config, err := g.renderStartupConfig(cameras)
		return config, false, err
	}
	if hadDisabled {
		applied := make([]store.Camera, 0, len(cameras))
		for _, camera := range cameras {
			if camera.PolicyState.AppliedRevision > 0 {
				applied = append(applied, camera)
			}
		}
		config, err := g.renderStartupConfig(applied)
		return config, false, err
	}
	for _, path := range []string{g.configPath + ".last-good", g.configPath} {
		config, err := os.ReadFile(path)
		if err == nil {
			return config, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
	}
	return nil, false, fmt.Errorf("pending camera policy has no last-known-good stream configuration")
}

func (g *Go2RTC) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, err := exec.LookPath(g.binary); err != nil {
		return err
	}
	if g.cmd != nil && g.cmd.Process != nil && g.cmd.ProcessState == nil {
		if healthy(ctx, g.apiURL) {
			return nil
		}
		_ = g.cmd.Process.Kill()
		g.cmd = nil
	}

	cmd := exec.Command(g.binary, "-config", g.configPath)
	cmd.Stdout = newRedactingLineWriter(os.Stdout)
	cmd.Stderr = newRedactingLineWriter(os.Stderr)
	if err := cmd.Start(); err != nil {
		return err
	}
	g.cmd = cmd
	go func() {
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if healthy(ctx, g.apiURL) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if g.cmd == cmd && cmd.Process != nil && cmd.ProcessState == nil {
		_ = cmd.Process.Kill()
		g.cmd = nil
	}
	return fmt.Errorf("go2rtc did not become healthy on %s", g.apiURL)
}

func (g *Go2RTC) Restart(ctx context.Context, cameras []store.Camera) error {
	config, _, err := g.renderPolicyConfig(cameras, false)
	if err != nil {
		return err
	}
	return g.ApplyConfig(ctx, config)
}

func (g *Go2RTC) ReconcileLiveWarmers(cameras []store.Camera) {
	if g.liveWarmer != nil {
		g.liveWarmer.Reconcile(cameras)
	}
}

func (g *Go2RTC) StopLiveWarmers() {
	if g.liveWarmer != nil {
		g.liveWarmer.StopAll()
	}
}

func (g *Go2RTC) restartProcess(ctx context.Context) error {
	g.mu.Lock()
	if g.cmd != nil && g.cmd.Process != nil && g.cmd.ProcessState == nil {
		_ = g.cmd.Process.Kill()
		g.cmd = nil
	}
	g.mu.Unlock()
	return g.Start(ctx)
}

func (g *Go2RTC) Status(ctx context.Context) Status {
	status := Status{APIURL: g.apiURL}
	snapshot := LiveWarmSnapshot{Active: map[string]bool{}}
	if g.liveWarmer != nil {
		snapshot = g.liveWarmer.Snapshot()
	}
	status.ExpectedLiveStreams = len(snapshot.Expected)
	if _, err := exec.LookPath(g.binary); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Installed = true
	status.Running = healthy(ctx, g.apiURL)
	if status.Running {
		runtime, err := fetchStreamRuntime(ctx, g.apiURL)
		if err != nil {
			status.Error = err.Error()
		} else {
			status.Streams = runtime
			status.ExpectedLiveStreams, status.ReadyLiveStreams, status.MediaReady = liveReadiness(snapshot, runtime)
		}
	}
	return status
}

func fetchStreamRuntime(ctx context.Context, apiURL string) (map[string]StreamRuntime, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/api/streams", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return nil, fmt.Errorf("go2rtc streams status returned %s", resp.Status)
	}
	return parseStreamRuntime(resp.Body)
}

type go2RTCProducer struct {
	ID         int      `json:"id"`
	FormatName string   `json:"format_name"`
	Protocol   string   `json:"protocol"`
	Medias     []string `json:"medias"`
}

type go2RTCConsumer struct {
	ID         int    `json:"id"`
	FormatName string `json:"format_name"`
	Protocol   string `json:"protocol"`
	UserAgent  string `json:"user_agent"`
}

type go2RTCStream struct {
	Producers []go2RTCProducer `json:"producers"`
	Consumers []go2RTCConsumer `json:"consumers"`
}

func parseStreamRuntime(reader io.Reader) (map[string]StreamRuntime, error) {
	var payload map[string]go2RTCStream
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return nil, err
	}

	runtime := make(map[string]StreamRuntime, len(payload))
	for streamName, stream := range payload {
		item := StreamRuntime{
			ProducerCount: activeProducerCount(stream.Producers),
			ConsumerCount: len(stream.Consumers),
		}
		for _, consumer := range stream.Consumers {
			if isViewerConsumer(consumer.FormatName, consumer.Protocol, consumer.UserAgent) {
				item.ViewerCount++
			}
		}
		item.State = runtimeState(item.ProducerCount, item.ConsumerCount)
		publicName := publicStreamName(streamName)
		if publicName == "" {
			continue
		}
		if existing, ok := runtime[publicName]; ok {
			item.ProducerCount += existing.ProducerCount
			item.ConsumerCount += existing.ConsumerCount
			item.ViewerCount += existing.ViewerCount
			item.State = runtimeState(item.ProducerCount, item.ConsumerCount)
		}
		runtime[publicName] = item
	}
	return runtime, nil
}

func activeProducerCount(producers []go2RTCProducer) int {
	count := 0
	for _, producer := range producers {
		if producer.ID != 0 || producer.FormatName != "" || producer.Protocol != "" || len(producer.Medias) > 0 {
			count++
		}
	}
	return count
}

func publicStreamName(streamName string) string {
	if strings.HasPrefix(streamName, privateSourcePrefix) {
		return ""
	}
	if strings.Contains(streamName, "://") {
		return store.RedactURL(streamName)
	}
	return streamName
}

func runtimeState(producerCount int, consumerCount int) string {
	switch {
	case producerCount > 0:
		return "running"
	case consumerCount > 0:
		return "starting"
	default:
		return "idle"
	}
}

func isViewerConsumer(formatName, protocol, userAgent string) bool {
	formatName = strings.ToLower(formatName)
	protocol = strings.ToLower(protocol)
	userAgent = strings.ToLower(userAgent)
	if strings.Contains(userAgent, "lavf") || strings.Contains(userAgent, "ffmpeg") {
		return false
	}
	return formatName == "mse/fmp4" || protocol == "ws" || strings.Contains(formatName, "webrtc")
}

func healthy(ctx context.Context, apiURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/api/streams", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func quoteYAML(value string) string {
	return strconv.Quote(value)
}

func localCandidates(port int) []string {
	var candidates []string
	interfaces, err := net.Interfaces()
	if err != nil {
		return candidates
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			candidates = append(candidates, fmt.Sprintf("%s:%d", ip.String(), port))
		}
	}
	return candidates
}
