package store

import "time"

type Event struct {
	ID        int64          `json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	Source    string         `json:"source"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

type Camera struct {
	ID                  int64                     `json:"id"`
	Name                string                    `json:"name"`
	URL                 string                    `json:"url,omitempty"`
	RedactedURL         string                    `json:"redactedUrl"`
	StreamName          string                    `json:"streamName"`
	LayoutKey           string                    `json:"layoutKey,omitempty"`
	RecordingStreamName string                    `json:"recordingStreamName,omitempty"`
	LiveStreamName      string                    `json:"liveStreamName,omitempty"`
	FocusStreamName     string                    `json:"focusStreamName,omitempty"`
	State               string                    `json:"state"`
	Enabled             bool                      `json:"enabled"`
	ProfileTemplateID   *int64                    `json:"profileTemplateId,omitempty"`
	Manufacturer        string                    `json:"manufacturer,omitempty"`
	Model               string                    `json:"model,omitempty"`
	ProfileAdapter      string                    `json:"profileAdapter,omitempty"`
	Host                string                    `json:"host,omitempty"`
	RTSPPort            int                       `json:"rtspPort,omitempty"`
	HTTPPort            int                       `json:"httpPort,omitempty"`
	ONVIFPort           int                       `json:"onvifPort,omitempty"`
	ChannelIndex        *int                      `json:"channelIndex,omitempty"`
	LastProbeJSON       map[string]any            `json:"lastProbe,omitempty"`
	LastScanJSON        map[string]any            `json:"lastScan,omitempty"`
	ControlCapabilities CameraControlCapabilities `json:"controlCapabilities"`
	Streams             []CameraStream            `json:"streams,omitempty"`
	Outputs             []CameraOutput            `json:"outputs,omitempty"`
	PolicyState         CameraPolicyState         `json:"policyState"`
	CreatedAt           time.Time                 `json:"createdAt"`
	UpdatedAt           time.Time                 `json:"updatedAt"`
}

type ControlSupport string

const (
	ControlSupportUnknown     ControlSupport = "unknown"
	ControlSupportSupported   ControlSupport = "supported"
	ControlSupportUnsupported ControlSupport = "unsupported"
)

type CameraControlFeature struct {
	Support   ControlSupport `json:"support"`
	Available bool           `json:"available"`
	Reason    string         `json:"reason,omitempty"`
}

type CameraControlCapabilities struct {
	PTZ          CameraControlFeature `json:"ptz"`
	Home         CameraControlFeature `json:"home"`
	Presets      CameraControlFeature `json:"presets"`
	Listen       CameraControlFeature `json:"listen"`
	Talk         CameraControlFeature `json:"talk"`
	Siren        CameraControlFeature `json:"siren"`
	MaxPresets   int                  `json:"maxPresets,omitempty"`
	DiscoveredAt string               `json:"discoveredAt,omitempty"`
}

func normalizeControlCapabilities(value CameraControlCapabilities) CameraControlCapabilities {
	features := []*CameraControlFeature{&value.PTZ, &value.Home, &value.Presets, &value.Listen, &value.Talk, &value.Siren}
	for _, feature := range features {
		switch feature.Support {
		case ControlSupportUnknown, ControlSupportSupported, ControlSupportUnsupported:
		default:
			feature.Support = ControlSupportUnknown
			feature.Available = false
		}
		if feature.Support != ControlSupportSupported {
			feature.Available = false
		}
	}
	if value.MaxPresets < 0 {
		value.MaxPresets = 0
	}
	return value
}

type CameraStreamRole string

const (
	CameraStreamRoleRecording CameraStreamRole = "recording"
	CameraStreamRoleLive      CameraStreamRole = "live"
	CameraStreamRoleSnapshot  CameraStreamRole = "snapshot"
)

type CameraStream struct {
	ID                  int64            `json:"id"`
	CameraID            int64            `json:"camera_id"`
	Role                CameraStreamRole `json:"role"`
	SourceKey           string           `json:"sourceKey"`
	Label               string           `json:"label"`
	Source              string           `json:"source"`
	URL                 string           `json:"url,omitempty"`
	RedactedURL         string           `json:"redactedUrl"`
	Go2RTCStreamName    string           `json:"go2rtcStreamName"`
	Codec               string           `json:"codec,omitempty"`
	Width               int              `json:"width,omitempty"`
	Height              int              `json:"height,omitempty"`
	FPS                 float64          `json:"fps,omitempty"`
	BitrateKbps         int              `json:"bitrateKbps,omitempty"`
	ProfileToken        string           `json:"profileToken,omitempty"`
	State               string           `json:"state,omitempty"`
	DetectedVideoCodec  string           `json:"detectedVideoCodec,omitempty"`
	DetectedAudioCodec  string           `json:"detectedAudioCodec,omitempty"`
	DetectedProfile     string           `json:"detectedProfile,omitempty"`
	DetectedLevel       string           `json:"detectedLevel,omitempty"`
	DetectedPixelFormat string           `json:"detectedPixelFormat,omitempty"`
	DetectedBitDepth    int              `json:"detectedBitDepth,omitempty"`
	DetectedWidth       int              `json:"detectedWidth,omitempty"`
	DetectedHeight      int              `json:"detectedHeight,omitempty"`
	DetectedFPS         float64          `json:"detectedFps,omitempty"`
	DetectedCheckedAt   time.Time        `json:"detectedCheckedAt,omitempty"`
	DetectedError       string           `json:"detectedError,omitempty"`
	CreatedAt           time.Time        `json:"createdAt,omitempty"`
	UpdatedAt           time.Time        `json:"updatedAt,omitempty"`
}

type CameraOutputPurpose string

const (
	CameraOutputRecording CameraOutputPurpose = "recording"
	CameraOutputLive      CameraOutputPurpose = "live"
	CameraOutputFocus     CameraOutputPurpose = "focus"
)

type CameraVideoMode string

const (
	CameraVideoAuto CameraVideoMode = "auto"
	CameraVideoCopy CameraVideoMode = "copy"
	CameraVideoH264 CameraVideoMode = "h264"
)

type CameraAudioMode string

const (
	CameraAudioSource CameraAudioMode = "source"
	CameraAudioNone   CameraAudioMode = "none"
	CameraAudioAAC    CameraAudioMode = "aac"
)

type CameraActivation string

const (
	CameraActivationOnDemand CameraActivation = "on_demand"
	CameraActivationAlways   CameraActivation = "always"
)

type CameraOutputPolicySnapshot struct {
	SourceStreamID int64            `json:"-"`
	SourceKey      string           `json:"sourceKey,omitempty"`
	VideoMode      CameraVideoMode  `json:"videoMode,omitempty"`
	MaxWidth       *int             `json:"maxWidth,omitempty"`
	MaxHeight      *int             `json:"maxHeight,omitempty"`
	MaxFPS         *float64         `json:"maxFps,omitempty"`
	AudioMode      CameraAudioMode  `json:"audioMode,omitempty"`
	Activation     CameraActivation `json:"activation,omitempty"`
}

type CameraOutputVerification struct {
	VideoCodec  string    `json:"videoCodec,omitempty"`
	AudioCodec  string    `json:"audioCodec,omitempty"`
	Width       int       `json:"width,omitempty"`
	Height      int       `json:"height,omitempty"`
	FPS         float64   `json:"fps,omitempty"`
	Transcoding bool      `json:"transcoding"`
	CheckedAt   time.Time `json:"checkedAt,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type CameraOutputApplyResult struct {
	Purpose      CameraOutputPurpose        `json:"purpose"`
	Policy       CameraOutputPolicySnapshot `json:"policy"`
	Verification CameraOutputVerification   `json:"verification"`
}

type CameraOutput struct {
	ID             int64                      `json:"id"`
	CameraID       int64                      `json:"cameraId"`
	Purpose        CameraOutputPurpose        `json:"purpose"`
	StreamName     string                     `json:"streamName"`
	SourceStreamID int64                      `json:"sourceStreamId"`
	SourceKey      string                     `json:"sourceKey"`
	VideoMode      CameraVideoMode            `json:"videoMode"`
	MaxWidth       *int                       `json:"maxWidth,omitempty"`
	MaxHeight      *int                       `json:"maxHeight,omitempty"`
	MaxFPS         *float64                   `json:"maxFps,omitempty"`
	AudioMode      CameraAudioMode            `json:"audioMode"`
	Activation     CameraActivation           `json:"activation"`
	AppliedPolicy  CameraOutputPolicySnapshot `json:"appliedPolicy"`
	Verification   CameraOutputVerification   `json:"verification"`
	CreatedAt      time.Time                  `json:"createdAt"`
	UpdatedAt      time.Time                  `json:"updatedAt"`
}

type CameraApplyState string

const (
	CameraApplyApplied CameraApplyState = "applied"
	CameraApplyPending CameraApplyState = "pending"
	CameraApplyFailed  CameraApplyState = "apply_failed"
)

type CameraPolicyState struct {
	CameraID        int64            `json:"cameraId"`
	DesiredRevision int64            `json:"desiredRevision"`
	AppliedRevision int64            `json:"appliedRevision"`
	ApplyState      CameraApplyState `json:"applyState"`
	ApplyStateAt    time.Time        `json:"applyStateAt"`
	AppliedAt       time.Time        `json:"appliedAt,omitempty"`
	ApplyError      string           `json:"applyError,omitempty"`
}

type LayoutItem struct {
	I         string     `json:"i"`
	X         int        `json:"x"`
	Y         int        `json:"y"`
	W         int        `json:"w"`
	H         int        `json:"h"`
	MinW      int        `json:"minW,omitempty"`
	MinH      int        `json:"minH,omitempty"`
	VideoZoom *VideoZoom `json:"videoZoom,omitempty"`
}

type VideoZoom struct {
	Scale float64 `json:"scale"`
	TX    float64 `json:"tx"`
	TY    float64 `json:"ty"`
}

type LayoutProfile struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Data              []LayoutItem `json:"data"`
	TimelineCollapsed bool         `json:"timeline_collapsed"`
	GridCols          int          `json:"grid_cols"`
	GridRows          *int         `json:"grid_rows"`
	CreatedAt         int64        `json:"created_at"`
	UpdatedAt         int64        `json:"updated_at"`
}

type RecordingSegment struct {
	ID          int64    `json:"id"`
	CameraID    int64    `json:"camera_id"`
	StreamName  string   `json:"streamName"`
	Filename    string   `json:"filename"`
	TempPath    string   `json:"tempPath,omitempty"`
	FinalPath   string   `json:"finalPath,omitempty"`
	TSStart     float64  `json:"ts_start"`
	TSEnd       *float64 `json:"ts_end"`
	FileSize    *int64   `json:"file_size"`
	Status      string   `json:"status"`
	BackupState string   `json:"backupState"`
	BackedUpAt  string   `json:"backedUpAt,omitempty"`
	BackupJobID int64    `json:"backupJobId,omitempty"`
	Error       string   `json:"error,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}
