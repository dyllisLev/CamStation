package stream

import (
	"fmt"
	"os"
	"time"

	"camstation/internal/ffmpegsupervisor"
	"camstation/internal/store"
	"gopkg.in/yaml.v3"
)

// EncoderRuntime separates deterministic reservations from observed child execution.
// No input addresses, local paths, command lines or raw diagnostics cross this boundary.
type EncoderRuntime struct {
	RequestedEncoder string    `json:"requestedEncoder" yaml:"requestedEncoder"`
	AllocatedEncoder string    `json:"allocatedEncoder" yaml:"allocatedEncoder"`
	ActualEncoder    string    `json:"actualEncoder,omitempty" yaml:"-"`
	State            string    `json:"state" yaml:"state"`
	Reason           string    `json:"reason,omitempty" yaml:"reason"`
	CheckedAt        time.Time `json:"checkedAt,omitempty" yaml:"-"`
}

func encoderOutputKey(cameraID int64, purpose store.CameraOutputPurpose) string {
	return fmt.Sprintf("%d_%s", cameraID, purpose)
}

// EncoderStatus reads allocation from the active configuration, not an uncommitted
// desired policy. Process evidence is accepted only while its supervisor is alive.
func (g *Go2RTC) EncoderStatus() map[string]EncoderRuntime {
	var config struct {
		Encoders map[string]EncoderRuntime `yaml:"camstation_encoders"`
	}
	data, err := os.ReadFile(g.configPath)
	if err != nil || yaml.Unmarshal(data, &config) != nil {
		return nil
	}
	if len(config.Encoders) == 0 {
		return nil
	}
	stateDir := os.Getenv("CAMSTATION_NVENC_STATE_DIR")
	if stateDir == "" {
		stateDir = ffmpegsupervisor.DefaultStateDir
	}
	configInfo, _ := os.Stat(g.configPath)
	observations, _ := ffmpegsupervisor.ReadStatuses(stateDir)
	for _, observed := range observations {
		item, ok := config.Encoders[observed.Output]
		if !ok {
			continue
		}
		if !observed.Running && configInfo != nil && observed.UpdatedAt.Before(configInfo.ModTime()) {
			continue
		}
		item.CheckedAt = observed.UpdatedAt
		item.State = "stopped"
		if observed.FallbackReason == "cpu_failed" {
			item.State = "failed"
		}
		if observed.Running {
			item.State = "running"
			item.ActualEncoder = observed.ActualEncoder
		}
		if observed.FallbackReason != "" {
			item.Reason = observed.FallbackReason
		}
		config.Encoders[observed.Output] = item
	}
	return config.Encoders
}
