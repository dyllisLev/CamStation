package stream

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"camstation/internal/store"
)

type Go2RTC struct {
	binary     string
	configPath string
	apiURL     string

	mu  sync.Mutex
	cmd *exec.Cmd
}

type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	APIURL    string `json:"apiUrl"`
	Error     string `json:"error,omitempty"`
}

func NewGo2RTC(configPath string) *Go2RTC {
	return &Go2RTC{
		binary:     "go2rtc",
		configPath: configPath,
		apiURL:     "http://127.0.0.1:1984",
	}
}

func (g *Go2RTC) Ensure(ctx context.Context, cameras []store.Camera) error {
	if err := g.WriteConfig(cameras); err != nil {
		return err
	}
	return g.Start(ctx)
}

func (g *Go2RTC) WriteConfig(cameras []store.Camera) error {
	if err := os.MkdirAll(filepath.Dir(g.configPath), 0o755); err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString("api:\n")
	buf.WriteString("  listen: 127.0.0.1:1984\n")
	buf.WriteString("rtsp:\n")
	buf.WriteString("  listen: 127.0.0.1:8554\n")
	buf.WriteString("webrtc:\n")
	buf.WriteString("  listen: 0.0.0.0:8555\n")
	candidates := localCandidates(8555)
	if len(candidates) > 0 {
		buf.WriteString("  candidates:\n")
		for _, candidate := range candidates {
			buf.WriteString(fmt.Sprintf("    - %s\n", quoteYAML(candidate)))
		}
	}
	buf.WriteString("streams:\n")
	if len(cameras) == 0 {
		buf.WriteString("  {}\n")
	} else {
		for _, camera := range cameras {
			if camera.URL == "" || camera.StreamName == "" {
				continue
			}
			buf.WriteString(fmt.Sprintf("  %s:\n", yamlKey(camera.StreamName)))
			buf.WriteString(fmt.Sprintf("    - %s\n", quoteYAML(camera.URL)))
		}
	}
	return os.WriteFile(g.configPath, buf.Bytes(), 0o600)
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
	return fmt.Errorf("go2rtc did not become healthy on %s", g.apiURL)
}

func (g *Go2RTC) Restart(ctx context.Context, cameras []store.Camera) error {
	g.mu.Lock()
	if g.cmd != nil && g.cmd.Process != nil && g.cmd.ProcessState == nil {
		_ = g.cmd.Process.Kill()
		g.cmd = nil
	}
	g.mu.Unlock()
	return g.Ensure(ctx, cameras)
}

func (g *Go2RTC) Status(ctx context.Context) Status {
	status := Status{APIURL: g.apiURL}
	if _, err := exec.LookPath(g.binary); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Installed = true
	status.Running = healthy(ctx, g.apiURL)
	return status
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

func yamlKey(value string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
}

func quoteYAML(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
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
