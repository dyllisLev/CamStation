package legacyimport

import "camstation/internal/store"

const ManifestVersion = 1

type Expectations struct {
	CameraCount     int
	EnabledCount    int
	SubStreamCount  int
	DisabledCamera  string
	LayoutCount     int
	LayoutItemCount int
	SegmentMinutes  int
	RetentionDays   int
	MaxStorageGB    float64
}

type Manifest struct {
	FormatVersion        int              `json:"formatVersion"`
	Operation            string           `json:"operation"`
	SourceKind           string           `json:"sourceKind,omitempty"`
	SourceFingerprint    string           `json:"sourceFingerprint,omitempty"`
	SelectionPrefix      string           `json:"selectionPrefix,omitempty"`
	QuickCheck           string           `json:"quickCheck"`
	SchemaCompatible     bool             `json:"schemaCompatible"`
	Ready                bool             `json:"ready"`
	TargetStatus         string           `json:"targetStatus,omitempty"`
	CanonicalFingerprint string           `json:"canonicalFingerprint,omitempty"`
	Summary              Summary          `json:"summary"`
	Cameras              []CameraManifest `json:"cameras"`
	Layouts              []LayoutManifest `json:"layouts"`
	Settings             SettingsManifest `json:"settings"`
	Warnings             []Finding        `json:"warnings,omitempty"`
	Blockers             []Finding        `json:"blockers,omitempty"`
}

type Summary struct {
	CameraCount      int `json:"cameraCount"`
	EnabledCount     int `json:"enabledCount"`
	DisabledCount    int `json:"disabledCount"`
	ArchivedCount    int `json:"archivedCount"`
	SubStreamCount   int `json:"subStreamCount"`
	LayoutCount      int `json:"layoutCount"`
	LayoutItemCount  int `json:"layoutItemCount"`
	DeferredControl  int `json:"deferredControlCount"`
	UnmappedMetadata int `json:"unmappedMetadataCount"`
}

type CameraManifest struct {
	Key                        string `json:"key"`
	Name                       string `json:"name"`
	Enabled                    bool   `json:"enabled"`
	HasSubStream               bool   `json:"hasSubStream"`
	MainURLFingerprint         string `json:"mainUrlFingerprint"`
	SubURLFingerprint          string `json:"subUrlFingerprint,omitempty"`
	LocationPresent            bool   `json:"locationPresent"`
	NotesPresent               bool   `json:"notesPresent"`
	ONVIFConfigured            bool   `json:"onvifConfigured"`
	ControlCredentialsDeferred bool   `json:"controlCredentialsDeferred"`
	SortOrder                  int    `json:"sortOrder"`
}

type LayoutManifest struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	SourceGridCols       int    `json:"sourceGridCols"`
	TargetGridCols       int    `json:"targetGridCols"`
	ItemCount            int    `json:"itemCount"`
	CanonicalFingerprint string `json:"canonicalFingerprint"`
}

type SettingsManifest struct {
	SegmentMinutes       int     `json:"segmentMinutes"`
	RetentionDays        int     `json:"retentionDays"`
	MaxStorageGB         float64 `json:"maxStorageGB"`
	MotionEnabled        bool    `json:"motionEnabled"`
	MotionThreshold      float64 `json:"motionThreshold"`
	BackupEnabled        bool    `json:"backupEnabled"`
	BackupTargetPresent  bool    `json:"backupTargetPresent"`
	ProtectUnbacked      bool    `json:"protectUnbacked"`
	MotionSettingsMapped bool    `json:"motionSettingsMapped"`
}

type Finding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type plan struct {
	manifest Manifest
	cameras  []plannedCamera
	layouts  []store.LayoutProfile
	settings store.SettingsUpdate
	canon    canonicalTarget
}

type plannedCamera struct {
	row store.Camera
}

type canonicalTarget struct {
	Cameras  []canonicalCamera `json:"cameras"`
	Layouts  []canonicalLayout `json:"layouts"`
	Settings canonicalSettings `json:"settings"`
}

type canonicalCamera struct {
	Key             string                  `json:"key"`
	Name            string                  `json:"name"`
	Enabled         bool                    `json:"enabled"`
	LayoutKey       string                  `json:"layoutKey"`
	Host            string                  `json:"host"`
	RTSPPort        int                     `json:"rtspPort"`
	HTTPPort        int                     `json:"httpPort"`
	ONVIFPort       int                     `json:"onvifPort"`
	DesiredRevision int64                   `json:"desiredRevision"`
	AppliedRevision int64                   `json:"appliedRevision"`
	ApplyState      store.CameraApplyState  `json:"applyState"`
	Inputs          []canonicalCameraInput  `json:"inputs"`
	Outputs         []canonicalCameraOutput `json:"outputs"`
}

type canonicalCameraInput struct {
	Role             store.CameraStreamRole `json:"role"`
	SourceKey        string                 `json:"sourceKey"`
	Label            string                 `json:"label"`
	Source           string                 `json:"source"`
	URL              string                 `json:"url"`
	Go2RTCStreamName string                 `json:"go2rtcStreamName"`
}

type canonicalCameraOutput struct {
	Purpose    store.CameraOutputPurpose `json:"purpose"`
	StreamName string                    `json:"streamName"`
	SourceKey  string                    `json:"sourceKey"`
	VideoMode  store.CameraVideoMode     `json:"videoMode"`
	MaxWidth   *int                      `json:"maxWidth,omitempty"`
	MaxHeight  *int                      `json:"maxHeight,omitempty"`
	MaxFPS     *float64                  `json:"maxFps,omitempty"`
	AudioMode  store.CameraAudioMode     `json:"audioMode"`
	Activation store.CameraActivation    `json:"activation"`
}

type canonicalLayout struct {
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	Data              []store.LayoutItem `json:"data"`
	TimelineCollapsed bool               `json:"timelineCollapsed"`
	GridCols          int                `json:"gridCols"`
	GridRows          *int               `json:"gridRows,omitempty"`
}

type canonicalSettings struct {
	Recording store.RecordingSettings `json:"recording"`
	Backup    store.BackupSettings    `json:"backup"`
}
