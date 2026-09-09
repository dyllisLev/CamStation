package stream

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/store"
	"gopkg.in/yaml.v3"
)

type encoderConfigFixture struct {
	Encoders map[string]EncoderRuntime `yaml:"camstation_encoders"`
	Streams  map[string][]string       `yaml:"streams"`
	FFmpeg   map[string]string         `yaml:"ffmpeg"`
}

func decodeEncoderConfig(t *testing.T, data []byte) encoderConfigFixture {
	t.Helper()
	var cfg encoderConfigFixture
	if yaml.Unmarshal(data, &cfg) != nil {
		t.Fatal("invalid generated config")
	}
	return cfg
}

func TestNVENCAllocationIsStableAndIncludesOnDemand(t *testing.T) {
	var cameras []store.Camera
	for _, id := range []int64{4, 2, 3, 1} {
		c, o := policyFixture("hevc", "yuv420p", 8, 1920, 1080, 30)
		c.ID = id
		c.StreamName = encoderOutputKey(id, store.CameraOutputLive)
		o.CameraID = id
		o.StreamName = c.StreamName
		o.VideoEncoder = store.CameraVideoEncoderNVENC
		c.Outputs = []store.CameraOutput{o}
		cameras = append(cameras, c)
	}
	data, _, err := renderPolicyConfigWithEncoder(cameras, false, nil, "camstation-ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeEncoderConfig(t, data)
	for _, c := range cameras {
		state := cfg.Encoders[encoderOutputKey(c.ID, store.CameraOutputFocus)]
		expected := "nvenc"
		if c.ID == 4 {
			expected = "cpu"
			if state.Reason != "session_limit" {
				t.Fatal("fourth reservation missing session limit reason")
			}
		}
		if state.AllocatedEncoder != expected || state.ActualEncoder != "" {
			t.Fatal("incorrect reservation or invented runtime evidence")
		}
		producers := cfg.Streams[c.StreamName]
		if len(producers) != 1 {
			t.Fatal("output must have exactly one shared producer")
		}
		if strings.Contains(producers[0], "#video=h264/nvenc") != (expected == "nvenc") {
			t.Fatal("encoder selector does not match reservation")
		}
	}
	if !strings.HasSuffix(cfg.FFmpeg["output"], "{output}#killsignal=15#killtimeout=2") {
		t.Fatal("supervisor must drain GPU diagnostics on go2rtc producer close")
	}
	if !strings.Contains(cfg.FFmpeg["h264/nvenc"], "-rc vbr -cq 23 -b:v 0") {
		t.Fatal("NVENC must preserve validated quality instead of implicit low bitrate")
	}
	if strings.Contains(cfg.FFmpeg["h264/nvenc"], "cuda") {
		t.Fatal("encoding must not enable CUDA decode")
	}
}

func TestNVENCSelectionPreservesCopyAndAppliedSnapshot(t *testing.T) {
	c, o := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	o.VideoEncoder = store.CameraVideoEncoderNVENC
	c.Outputs = []store.CameraOutput{o}
	data, _, err := renderPolicyConfigWithEncoder([]store.Camera{c}, false, nil, "camstation-ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeEncoderConfig(t, data)
	if cfg.Encoders["1_focus"].AllocatedEncoder != "copy" || strings.Contains(cfg.Streams[o.StreamName][0], "h264/nvenc") {
		t.Fatal("auto-safe input must remain copy")
	}
	o.VideoMode = store.CameraVideoH264
	o.AppliedPolicy = store.CameraOutputPolicySnapshot{SourceStreamID: o.SourceStreamID, SourceKey: o.SourceKey, VideoMode: store.CameraVideoH264, VideoEncoder: store.CameraVideoEncoderCPU, AudioMode: o.AudioMode, Activation: o.Activation}
	o.Verification.Transcoding = true
	c.Outputs = []store.CameraOutput{o}
	c.PolicyState.AppliedRevision = 1
	data, results, err := renderPolicyConfigWithEncoder([]store.Camera{c}, true, nil, "camstation-ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	cfg = decodeEncoderConfig(t, data)
	if cfg.Encoders["1_focus"].AllocatedEncoder != "cpu" || results[c.ID][0].Policy.VideoEncoder != store.CameraVideoEncoderCPU {
		t.Fatal("pending desired GPU policy activated on startup")
	}
}

func TestNVENCWithoutSupervisorSafelyAllocatesCPU(t *testing.T) {
	c, o := policyFixture("hevc", "yuv420p", 8, 1920, 1080, 20)
	o.VideoEncoder = store.CameraVideoEncoderNVENC
	c.Outputs = []store.CameraOutput{o}
	data, _, err := renderPolicyConfigWithEncoder([]store.Camera{c}, false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := decodeEncoderConfig(t, data)
	if cfg.Encoders["1_focus"].Reason != "supervisor_unavailable" || strings.Contains(cfg.Streams[o.StreamName][0], "camstation-output") {
		t.Fatal("missing supervisor must safely use native CPU command")
	}
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if os.WriteFile(path, data, 0600) != nil {
		t.Fatal("write fixture")
	}
	t.Setenv("CAMSTATION_NVENC_STATE_DIR", t.TempDir())
	status := NewGo2RTC(path).EncoderStatus()["1_focus"]
	if status.ActualEncoder != "" || status.State != "unverified" {
		t.Fatal("allocation is not proof of actual encoding")
	}
}
