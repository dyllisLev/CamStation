package store

import (
	"errors"
	"time"
)

var (
	ErrNotFound                 = errors.New("not found")
	ErrProfileTemplateDuplicate = errors.New("camera profile template duplicate")
	ErrProfileTemplateInUse     = errors.New("camera profile template in use")
	ErrProfileTemplateInvalid   = errors.New("camera profile template invalid")
)

type CameraProfileTemplate struct {
	ID           int64                          `json:"id"`
	ProfileName  string                         `json:"profileName"`
	Manufacturer string                         `json:"manufacturer"`
	Model        string                         `json:"model"`
	Adapter      string                         `json:"adapter"`
	Version      int                            `json:"version"`
	MatchRules   []CameraProfileMatchRule       `json:"matchRules"`
	Channels     []CameraProfileTemplateChannel `json:"channels"`
	Capabilities CameraProfileCapabilities      `json:"capabilities"`
	CreatedAt    time.Time                      `json:"createdAt"`
	UpdatedAt    time.Time                      `json:"updatedAt"`
}

type CameraProfileMatchRule struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type CameraProfileTemplateChannel struct {
	Index   int                           `json:"index"`
	Name    string                        `json:"name"`
	Streams []CameraProfileTemplateStream `json:"streams"`
}

type CameraProfileTemplateStream struct {
	Role         CameraStreamRole `json:"role"`
	Label        string           `json:"label"`
	Source       string           `json:"source"`
	Path         string           `json:"path"`
	ProfileToken string           `json:"profileToken,omitempty"`
	Codec        string           `json:"codec,omitempty"`
	Width        int              `json:"width,omitempty"`
	Height       int              `json:"height,omitempty"`
	FPS          float64          `json:"fps,omitempty"`
	BitrateKbps  int              `json:"bitrateKbps,omitempty"`
}

type CameraProfileCapabilities struct {
	ONVIF        bool `json:"onvif,omitempty"`
	RTSP         bool `json:"rtsp,omitempty"`
	Snapshot     bool `json:"snapshot,omitempty"`
	MultiChannel bool `json:"multiChannel,omitempty"`
}
