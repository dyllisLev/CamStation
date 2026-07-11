package cameraprofile

import "context"

type StreamRole string

const (
	StreamRoleRecording StreamRole = "recording"
	StreamRoleLive      StreamRole = "live"
	StreamRoleSnapshot  StreamRole = "snapshot"
)

type ScanRequest struct {
	Name      string `json:"name,omitempty"`
	URL       string `json:"url,omitempty"`
	Host      string `json:"host"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	RTSPPort  int    `json:"rtspPort,omitempty"`
	HTTPPort  int    `json:"httpPort,omitempty"`
	ONVIFPort int    `json:"onvifPort,omitempty"`
	Adapter   string `json:"adapter,omitempty"`
}

type DeviceProfile struct {
	Name         string           `json:"name,omitempty"`
	Host         string           `json:"host"`
	Manufacturer string           `json:"manufacturer"`
	Model        string           `json:"model"`
	Adapter      string           `json:"adapter"`
	RTSPPort     int              `json:"rtspPort,omitempty"`
	HTTPPort     int              `json:"httpPort,omitempty"`
	ONVIFPort    int              `json:"onvifPort,omitempty"`
	Capabilities Capabilities     `json:"capabilities"`
	Channels     []ChannelProfile `json:"channels"`
	LastScan     map[string]any   `json:"lastScan,omitempty"`
}

type Capabilities struct {
	PTZ        bool `json:"ptz"`
	Audio      bool `json:"audio"`
	Microphone bool `json:"microphone"`
	Speaker    bool `json:"speaker"`
	Siren      bool `json:"siren"`
	MaxPresets int  `json:"maxPresets,omitempty"`
}

type ChannelProfile struct {
	Index      int               `json:"index"`
	Label      string            `json:"label"`
	Candidates []StreamCandidate `json:"candidates"`
}

type StreamCandidate struct {
	RoleHint     StreamRole `json:"roleHint"`
	Label        string     `json:"label"`
	Source       string     `json:"source"`
	URL          string     `json:"url,omitempty"`
	RedactedURL  string     `json:"redactedUrl,omitempty"`
	ProducerKey  string     `json:"producerKey,omitempty"`
	Codec        string     `json:"codec,omitempty"`
	Width        int        `json:"width,omitempty"`
	Height       int        `json:"height,omitempty"`
	FPS          float64    `json:"fps,omitempty"`
	BitrateKbps  int        `json:"bitrateKbps,omitempty"`
	ProfileToken string     `json:"profileToken,omitempty"`
}

type PTZSummary struct {
	Supported  bool
	MaxPresets int
}

type ScannerClient interface {
	DeviceInformation(context.Context, ScanRequest) (string, error)
	Hostname(context.Context, ScanRequest) (string, error)
	Profiles(context.Context, ScanRequest) (string, error)
	StreamURI(context.Context, ScanRequest, string) (string, error)
	PTZSummary(context.Context, ScanRequest, string) (PTZSummary, error)
}

type Scanner struct {
	client ScannerClient
}

func NewScanner(client ScannerClient) Scanner {
	return Scanner{client: client}
}

func (s Scanner) Scan(ctx context.Context, req ScanRequest) (DeviceProfile, error) {
	result, err := s.ScanResult(ctx, req)
	if err != nil {
		return DeviceProfile{}, err
	}
	return result.DeviceProfile(req.Name), nil
}
