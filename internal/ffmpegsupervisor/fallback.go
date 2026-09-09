package ffmpegsupervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// A go2rtc restart/apply replaces the parent generation and permits a fresh
// NVENC attempt. Reconnecting viewers within one generation reuse the CPU
// decision made for this output after a GPU failure.
type parentGeneration struct {
	PID           int    `json:"parentPID"`
	StartIdentity string `json:"parentStartIdentity"`
}
type fallbackRecord struct {
	Parent parentGeneration `json:"parent"`
	Reason string           `json:"reason"`
}

func currentParent() parentGeneration {
	pid := os.Getppid()
	return parentGeneration{PID: pid, StartIdentity: processIdentity(pid)}
}
func readFallback(dir, output string, parent parentGeneration) string {
	if parent.StartIdentity == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, output+".fallback"))
	if err != nil {
		return ""
	}
	var r fallbackRecord
	if json.Unmarshal(b, &r) != nil || r.Parent != parent {
		return ""
	}
	switch r.Reason {
	case "encoder_failed", "api_incompatible", "library_unavailable", "permission_denied", "device_unavailable", "probe_timeout":
		return r.Reason
	}
	return ""
}
func writeFallback(dir, output string, parent parentGeneration, reason string) {
	if parent.StartIdentity == "" || !outputPattern.MatchString(output) {
		return
	}
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	b, err := json.Marshal(fallbackRecord{Parent: parent, Reason: reason})
	if err != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".fallback-")
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
	_ = os.Rename(name, filepath.Join(dir, output+".fallback"))
}
