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
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	URL                 string         `json:"url,omitempty"`
	RedactedURL         string         `json:"redactedUrl"`
	StreamName          string         `json:"streamName"`
	LayoutKey           string         `json:"layoutKey,omitempty"`
	RecordingStreamName string         `json:"recordingStreamName,omitempty"`
	LiveStreamName      string         `json:"liveStreamName,omitempty"`
	State               string         `json:"state"`
	ProfileTemplateID   *int64         `json:"profileTemplateId,omitempty"`
	Manufacturer        string         `json:"manufacturer,omitempty"`
	Model               string         `json:"model,omitempty"`
	ProfileAdapter      string         `json:"profileAdapter,omitempty"`
	Host                string         `json:"host,omitempty"`
	RTSPPort            int            `json:"rtspPort,omitempty"`
	HTTPPort            int            `json:"httpPort,omitempty"`
	ONVIFPort           int            `json:"onvifPort,omitempty"`
	ChannelIndex        *int           `json:"channelIndex,omitempty"`
	LastProbeJSON       map[string]any `json:"lastProbe,omitempty"`
	LastScanJSON        map[string]any `json:"lastScan,omitempty"`
	Streams             []CameraStream `json:"streams,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type CameraStreamRole string

const (
	CameraStreamRoleRecording CameraStreamRole = "recording"
	CameraStreamRoleLive      CameraStreamRole = "live"
	CameraStreamRoleSnapshot  CameraStreamRole = "snapshot"
)

type CameraStream struct {
	ID               int64            `json:"id"`
	CameraID         int64            `json:"camera_id"`
	Role             CameraStreamRole `json:"role"`
	Label            string           `json:"label"`
	Source           string           `json:"source"`
	URL              string           `json:"url,omitempty"`
	RedactedURL      string           `json:"redactedUrl"`
	Go2RTCStreamName string           `json:"go2rtcStreamName"`
	Codec            string           `json:"codec,omitempty"`
	Width            int              `json:"width,omitempty"`
	Height           int              `json:"height,omitempty"`
	FPS              float64          `json:"fps,omitempty"`
	BitrateKbps      int              `json:"bitrateKbps,omitempty"`
	ProfileToken     string           `json:"profileToken,omitempty"`
	State            string           `json:"state,omitempty"`
	CreatedAt        time.Time        `json:"createdAt,omitempty"`
	UpdatedAt        time.Time        `json:"updatedAt,omitempty"`
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
