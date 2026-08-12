package cameraprofile

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	vstarcamStreamPathRE = regexp.MustCompile(`(?i)(?:^|/)av([0-9]+)_([0-9]+)(?:$|[^0-9])`)
	reolinkPreviewPathRE = regexp.MustCompile(`(?i)preview_([0-9]+)_`)
	reolinkChannelRE     = regexp.MustCompile(`(?i)channel([0-9]+)_`)
)

type DeviceScanResult struct {
	Host         string         `json:"host"`
	Manufacturer string         `json:"manufacturer"`
	Model        string         `json:"model"`
	Adapter      string         `json:"adapter"`
	RTSPPort     int            `json:"rtspPort,omitempty"`
	HTTPPort     int            `json:"httpPort,omitempty"`
	ONVIFPort    int            `json:"onvifPort,omitempty"`
	Capabilities Capabilities   `json:"capabilities"`
	Channels     []ScanChannel  `json:"channels"`
	LastScan     map[string]any `json:"lastScan,omitempty"`
}

type ScanChannel struct {
	Index      int               `json:"index"`
	Label      string            `json:"label"`
	Candidates []StreamCandidate `json:"candidates"`
}

func (s Scanner) ScanResult(ctx context.Context, req ScanRequest) (DeviceScanResult, error) {
	if s.client == nil {
		s.client = NewNetworkScannerClient()
	}
	req = normalizeRequest(req)
	if req.Host == "" {
		return DeviceScanResult{}, fmt.Errorf("host is required")
	}

	deviceXML, deviceErr := s.client.DeviceInformation(ctx, req)
	hostname, _ := s.client.Hostname(ctx, req)
	profilesXML, profilesErr := s.client.Profiles(ctx, req)
	if deviceErr != nil && profilesErr != nil {
		return DeviceScanResult{}, fmt.Errorf("onvif scan failed: %w", profilesErr)
	}

	device := parseDeviceInformation(deviceXML)
	profiles, err := parseProfiles(profilesXML)
	if err != nil {
		return DeviceScanResult{}, err
	}
	if len(profiles) == 0 {
		return DeviceScanResult{}, fmt.Errorf("no ONVIF media profiles returned")
	}

	streamURIs := map[string]string{}
	for i, profile := range profiles {
		streamURI, err := s.client.StreamURI(ctx, req, profile.Token)
		if err != nil || streamURI == "" {
			streamURI = derivedVStarcamURI(req, i)
		}
		streamURIs[profile.Token] = withCredentials(streamURI, req.Username, req.Password)
	}

	adapter := detectAdapter(req.Adapter, device.Manufacturer, device.Model, hostname, streamURIs)
	if adapter == "" {
		return DeviceScanResult{}, fmt.Errorf("no supported camera profile detected")
	}

	identity := normalizeIdentity(adapter, device)
	capabilities := capabilitiesFromProfiles(profiles)
	if ptz, err := s.client.PTZSummary(ctx, req, firstToken(profiles)); err == nil && ptz.Supported {
		capabilities.PTZ = true
		capabilities.MaxPresets = ptz.MaxPresets
	}

	candidates := make([]StreamCandidate, 0, len(profiles))
	for i, profile := range profiles {
		streamURI := streamURIs[profile.Token]
		if streamURI == "" {
			continue
		}
		role := streamRoleForProfile(profile, streamURI, i)
		label := labelForProfile(profile, role)
		source := "onvif"
		if adapter == "vstarcam" {
			source = "onvif-vstarcam"
		}
		candidates = append(candidates, StreamCandidate{
			RoleHint:     role,
			Label:        label,
			Source:       source,
			URL:          streamURI,
			RedactedURL:  redactURL(streamURI),
			Codec:        strings.ToLower(profile.Encoding),
			Width:        profile.Width,
			Height:       profile.Height,
			FPS:          roundFPS(profile.FrameRate),
			BitrateKbps:  profile.BitrateKbps,
			ProfileToken: profile.Token,
		})
	}
	if isReolinkAdapter(adapter) {
		candidates = appendReolinkClearHTTPFLVCandidate(req, candidates)
	}
	if len(candidates) == 0 {
		return DeviceScanResult{}, fmt.Errorf("no playable stream candidates detected")
	}

	return DeviceScanResult{
		Host:         req.Host,
		Manufacturer: identity.Manufacturer,
		Model:        identity.Model,
		Adapter:      adapter,
		RTSPPort:     req.RTSPPort,
		HTTPPort:     req.HTTPPort,
		ONVIFPort:    req.ONVIFPort,
		Capabilities: capabilities,
		Channels:     groupScanCandidates(candidates),
		LastScan: map[string]any{
			"adapter":                adapter,
			"hostname":               hostname,
			"onvifDeviceInformation": device,
			"onvifProfiles":          summarizeProfiles(profiles),
			"detectedAt":             time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

func (r DeviceScanResult) DeviceProfile(name string) DeviceProfile {
	channels := make([]ChannelProfile, 0, len(r.Channels))
	for _, channel := range r.Channels {
		channels = append(channels, ChannelProfile{
			Index:      channel.Index,
			Label:      channel.Label,
			Candidates: channel.Candidates,
		})
	}
	return DeviceProfile{
		Name:         name,
		Host:         r.Host,
		Manufacturer: r.Manufacturer,
		Model:        r.Model,
		Adapter:      r.Adapter,
		RTSPPort:     r.RTSPPort,
		HTTPPort:     r.HTTPPort,
		ONVIFPort:    r.ONVIFPort,
		Capabilities: r.Capabilities,
		Channels:     channels,
		LastScan:     r.LastScan,
	}
}

func groupScanCandidates(candidates []StreamCandidate) []ScanChannel {
	byIndex := map[int][]StreamCandidate{}
	for _, candidate := range candidates {
		index := channelIndexForCandidate(candidate)
		byIndex[index] = append(byIndex[index], candidate)
	}

	indexes := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	channels := make([]ScanChannel, 0, len(indexes))
	for _, index := range indexes {
		channels = append(channels, ScanChannel{
			Index:      index,
			Label:      fmt.Sprintf("channel %d", index),
			Candidates: byIndex[index],
		})
	}
	return channels
}

func streamRoleForProfile(profile profileInfo, streamURI string, index int) StreamRole {
	value := strings.ToLower(profile.Token + " " + profile.Name + " " + streamURI)
	if strings.Contains(value, "snapshot") {
		return StreamRoleSnapshot
	}
	if _, stream, ok := vstarcamChannelAndStream(value); ok {
		if stream == 1 {
			return StreamRoleLive
		}
		return StreamRoleRecording
	}
	switch {
	case strings.Contains(value, "profile_001"), strings.Contains(value, "sub"):
		return StreamRoleLive
	case strings.Contains(value, "profile_000"), strings.Contains(value, "main"):
		return StreamRoleRecording
	case index == 1:
		return StreamRoleLive
	default:
		return StreamRoleRecording
	}
}
