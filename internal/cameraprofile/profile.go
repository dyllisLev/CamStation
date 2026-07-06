package cameraprofile

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

type deviceInfo struct {
	Manufacturer    string `json:"manufacturer,omitempty"`
	Model           string `json:"model,omitempty"`
	FirmwareVersion string `json:"firmwareVersion,omitempty"`
	SerialNumber    string `json:"serialNumber,omitempty"`
	HardwareID      string `json:"hardwareId,omitempty"`
}

type profileInfo struct {
	Token       string
	Name        string
	Encoding    string
	Width       int
	Height      int
	FrameRate   float64
	BitrateKbps int
	HasAudio    bool
	HasPTZ      bool
}

func normalizeRequest(req ScanRequest) ScanRequest {
	if req.URL != "" {
		if parsed, err := url.Parse(req.URL); err == nil {
			if req.Host == "" {
				req.Host = parsed.Hostname()
			}
			if parsed.User != nil {
				if req.Username == "" {
					req.Username = parsed.User.Username()
				}
				if req.Password == "" {
					req.Password, _ = parsed.User.Password()
				}
			}
			if parsed.Port() != "" && req.RTSPPort == 0 {
				req.RTSPPort, _ = strconv.Atoi(parsed.Port())
			}
		}
	}
	if req.Adapter == "" {
		req.Adapter = "auto"
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = 554
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = 80
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = req.HTTPPort
	}
	return req
}

func parseDeviceInformation(raw string) deviceInfo {
	return deviceInfo{
		Manufacturer:    textByLocalName(raw, "Manufacturer"),
		Model:           textByLocalName(raw, "Model"),
		FirmwareVersion: textByLocalName(raw, "FirmwareVersion"),
		SerialNumber:    textByLocalName(raw, "SerialNumber"),
		HardwareID:      textByLocalName(raw, "HardwareId"),
	}
}

func parseProfiles(raw string) ([]profileInfo, error) {
	type rawProfile struct {
		Token string `xml:"token,attr"`
		Name  string `xml:"Name"`
		Video struct {
			Token      string `xml:"token,attr"`
			Encoding   string `xml:"Encoding"`
			Resolution struct {
				Width  int `xml:"Width"`
				Height int `xml:"Height"`
			} `xml:"Resolution"`
			RateControl struct {
				FrameRateLimit float64 `xml:"FrameRateLimit"`
				BitrateLimit   int     `xml:"BitrateLimit"`
			} `xml:"RateControl"`
		} `xml:"VideoEncoderConfiguration"`
		Audio *struct {
			Encoding string `xml:"Encoding"`
		} `xml:"AudioEncoderConfiguration"`
		PTZ *struct{} `xml:"PTZConfiguration"`
	}

	decoder := xml.NewDecoder(strings.NewReader(xmlDocument(raw)))
	profiles := make([]profileInfo, 0)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse ONVIF profiles: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Profiles" {
			continue
		}
		var item rawProfile
		if err := decoder.DecodeElement(&item, &start); err != nil {
			return nil, fmt.Errorf("parse ONVIF profile: %w", err)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.Token
		}
		profiles = append(profiles, profileInfo{
			Token:       item.Token,
			Name:        name,
			Encoding:    item.Video.Encoding,
			Width:       item.Video.Resolution.Width,
			Height:      item.Video.Resolution.Height,
			FrameRate:   item.Video.RateControl.FrameRateLimit,
			BitrateKbps: item.Video.RateControl.BitrateLimit,
			HasAudio:    item.Audio != nil,
			HasPTZ:      item.PTZ != nil,
		})
	}
	return profiles, nil
}

func capabilitiesFromProfiles(profiles []profileInfo) Capabilities {
	var capabilities Capabilities
	for _, profile := range profiles {
		capabilities.Audio = capabilities.Audio || profile.HasAudio
		capabilities.Microphone = capabilities.Microphone || profile.HasAudio
		capabilities.PTZ = capabilities.PTZ || profile.HasPTZ
	}
	return capabilities
}

func detectAdapter(requested, manufacturer, model, hostname string, streamURIs map[string]string) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested != "" && requested != "auto" {
		return requested
	}
	fingerprint := strings.ToLower(strings.Join([]string{manufacturer, model, hostname}, " "))
	for _, uri := range streamURIs {
		fingerprint += " " + strings.ToLower(uri)
	}
	if strings.Contains(fingerprint, "reolink") {
		return "reolink"
	}
	if strings.Contains(fingerprint, "vstarcam") ||
		strings.Contains(fingerprint, "veepai") ||
		strings.Contains(fingerprint, "/tcp/av0_") {
		return "vstarcam"
	}
	if len(streamURIs) > 0 && (manufacturer != "" || model != "") {
		return "onvif"
	}
	return ""
}

func normalizeIdentity(adapter string, info deviceInfo) deviceInfo {
	if adapter != "vstarcam" {
		return info
	}
	manufacturer := strings.TrimSpace(info.Manufacturer)
	model := strings.TrimSpace(info.Model)
	if manufacturer == "" || strings.EqualFold(manufacturer, "IP camera") {
		manufacturer = "VStarcam"
	}
	if model == "" || strings.EqualFold(model, "IP Camera") {
		model = "VeePai IP Camera"
	}
	info.Manufacturer = manufacturer
	info.Model = model
	return info
}

func firstToken(profiles []profileInfo) string {
	if len(profiles) == 0 {
		return ""
	}
	return profiles[0].Token
}

func roleForProfile(profile profileInfo, streamURI string, index int) StreamRole {
	value := strings.ToLower(profile.Token + " " + profile.Name + " " + streamURI)
	switch {
	case strings.Contains(value, "av0_1"), strings.Contains(value, "profile_001"), strings.Contains(value, "sub"):
		return StreamRoleLive
	case strings.Contains(value, "snapshot"):
		return StreamRoleSnapshot
	case strings.Contains(value, "av0_0"), strings.Contains(value, "profile_000"), strings.Contains(value, "main"):
		return StreamRoleRecording
	case index == 1:
		return StreamRoleLive
	default:
		return StreamRoleRecording
	}
}

func labelForProfile(profile profileInfo, role StreamRole) string {
	suffix := "main"
	if role == StreamRoleLive {
		suffix = "sub"
	} else if role == StreamRoleSnapshot {
		suffix = "snapshot"
	}
	if profile.Token != "" {
		return profile.Token + " " + suffix
	}
	if profile.Name != "" {
		return profile.Name + " " + suffix
	}
	return suffix
}

func summarizeProfiles(profiles []profileInfo) []map[string]any {
	out := make([]map[string]any, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, map[string]any{
			"token":       profile.Token,
			"name":        profile.Name,
			"encoding":    profile.Encoding,
			"width":       profile.Width,
			"height":      profile.Height,
			"frameRate":   profile.FrameRate,
			"bitrateKbps": profile.BitrateKbps,
			"audio":       profile.HasAudio,
			"ptz":         profile.HasPTZ,
		})
	}
	return out
}
