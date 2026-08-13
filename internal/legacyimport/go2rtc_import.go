package legacyimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"

	"camstation/internal/store"

	"gopkg.in/yaml.v3"
)

const maxGo2RTCConfigBytes = 4 << 20

type Go2RTCCanaryOptions struct {
	Prefix          string
	ExpectedCameras int
	SegmentMinutes  int
	RetentionDays   int
	MaxStorageGB    float64
}

func ImportGo2RTCCanary(ctx context.Context, source, target string, options Go2RTCCanaryOptions) (Manifest, error) {
	prepared, err := buildGo2RTCCanaryPlan(source, options)
	prepared.manifest.Operation = "go2rtc-canary"
	if err != nil || !prepared.manifest.Ready {
		return prepared.manifest, err
	}
	return promotePlan(ctx, source, target, prepared)
}

func buildGo2RTCCanaryPlan(source string, options Go2RTCCanaryOptions) (plan, error) {
	result := plan{manifest: Manifest{
		FormatVersion: ManifestVersion,
		Operation:     "go2rtc-canary",
		SourceKind:    "go2rtc-yaml",
		QuickCheck:    "not-applicable",
	}}
	content, err := readGo2RTCConfig(source)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(content)
	result.manifest.SourceFingerprint = hex.EncodeToString(sum[:])

	options = normalizeGo2RTCCanaryOptions(options)
	result.manifest.SelectionPrefix = options.Prefix
	if strings.TrimSpace(options.Prefix) == "" || options.Prefix != strings.TrimSpace(options.Prefix) ||
		!validLegacyCameraKey(options.Prefix+"canary") {
		result.manifest.Blockers = append(result.manifest.Blockers, Finding{
			Code: "SELECTION_PREFIX_INVALID", Message: "the go2rtc canary selection prefix is invalid",
		})
	}
	if options.ExpectedCameras <= 0 {
		result.manifest.Blockers = append(result.manifest.Blockers, Finding{
			Code: "SELECTION_COUNT_INVALID", Message: "the expected go2rtc canary camera count must be positive",
		})
	}

	streams, parseBlockers := parseGo2RTCStreams(content)
	result.manifest.Blockers = append(result.manifest.Blockers, parseBlockers...)
	result.manifest.SchemaCompatible = len(parseBlockers) == 0
	if !result.manifest.SchemaCompatible || len(result.manifest.Blockers) > 0 {
		sortFindings(result.manifest.Blockers)
		return result, nil
	}

	selected := make([]string, 0, options.ExpectedCameras)
	selectedSet := make(map[string]bool)
	for _, item := range streams.order {
		if !strings.HasPrefix(item, options.Prefix) || strings.HasSuffix(item, "_sub") {
			continue
		}
		selected = append(selected, item)
		selectedSet[item] = true
	}
	for _, item := range streams.order {
		if !strings.HasPrefix(item, options.Prefix) || !strings.HasSuffix(item, "_sub") {
			continue
		}
		base := strings.TrimSuffix(item, "_sub")
		if !selectedSet[base] {
			result.manifest.Blockers = append(result.manifest.Blockers, Finding{
				Code: "ORPHAN_SUB_STREAM", Message: "a selected go2rtc sub stream has no matching main stream",
			})
		}
	}
	if len(selected) != options.ExpectedCameras {
		result.manifest.Blockers = append(result.manifest.Blockers, Finding{
			Code:    "SELECTION_COUNT_MISMATCH",
			Message: fmt.Sprintf("go2rtc canary selection found %d camera(s), expected %d", len(selected), options.ExpectedCameras),
		})
	}

	for index, key := range selected {
		mainURL, mainOK := validatedGo2RTCProducer(streams.values[key])
		subURL, subOK := validatedGo2RTCProducer(streams.values[key+"_sub"])
		if !validLegacyCameraKey(key) {
			result.manifest.Blockers = append(result.manifest.Blockers, Finding{
				Code: "CAMERA_KEY_INVALID", Message: "a selected go2rtc camera key is unsafe",
			})
		}
		if !mainOK {
			result.manifest.Blockers = append(result.manifest.Blockers, Finding{
				Code: "CAMERA_MAIN_PRODUCER_INVALID", Message: "camera " + key + " must have exactly one direct non-loopback main URL",
			})
		}
		if !subOK {
			result.manifest.Blockers = append(result.manifest.Blockers, Finding{
				Code: "CAMERA_SUB_PRODUCER_INVALID", Message: "camera " + key + " must have exactly one direct non-loopback sub URL",
			})
		}
		if !mainOK || !subOK || !validLegacyCameraKey(key) {
			continue
		}
		row := go2RTCCanaryCamera(key, mainURL, subURL)
		result.cameras = append(result.cameras, plannedCamera{row: row})
		result.manifest.Cameras = append(result.manifest.Cameras, CameraManifest{
			Key: key, Name: key, Enabled: true, HasSubStream: true,
			MainURLFingerprint: secretFingerprint(mainURL), SubURLFingerprint: secretFingerprint(subURL), SortOrder: index,
		})
		result.canon.Cameras = append(result.canon.Cameras, canonicalCameraFromStore(row))
	}

	result.layouts = []store.LayoutProfile{go2RTCCanaryLayout(selected)}
	result.settings = go2RTCCanarySettings(options)
	result.canon.Layouts = []canonicalLayout{canonicalLayoutFromStore(result.layouts[0])}
	result.canon.Settings = canonicalSettings{Recording: *result.settings.Recording, Backup: *result.settings.Backup}
	result.manifest.Summary = Summary{
		CameraCount: len(result.cameras), EnabledCount: len(result.cameras), SubStreamCount: len(result.cameras),
		LayoutCount: 1, LayoutItemCount: len(selected),
	}
	result.manifest.Settings = SettingsManifest{
		SegmentMinutes: options.SegmentMinutes, RetentionDays: options.RetentionDays, MaxStorageGB: options.MaxStorageGB,
		BackupEnabled: false, BackupTargetPresent: false, ProtectUnbacked: true,
	}
	if len(result.manifest.Blockers) == 0 && len(result.cameras) == options.ExpectedCameras {
		result.manifest.Ready = true
		result.manifest.CanonicalFingerprint = canonicalFingerprint(result.canon)
	}
	sortFindings(result.manifest.Blockers)
	return result, nil
}

func normalizeGo2RTCCanaryOptions(options Go2RTCCanaryOptions) Go2RTCCanaryOptions {
	if options.SegmentMinutes <= 0 {
		options.SegmentMinutes = 1
	}
	if options.RetentionDays <= 0 {
		options.RetentionDays = 1
	}
	if options.MaxStorageGB <= 0 {
		options.MaxStorageGB = 20
	}
	return options
}

func readGo2RTCConfig(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("go2rtc source must be an existing regular, non-symlink file")
	}
	if info.Size() <= 0 || info.Size() > maxGo2RTCConfigBytes {
		return nil, fmt.Errorf("go2rtc source size is outside the allowed range")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read go2rtc source: %w", err)
	}
	return content, nil
}

type go2RTCStreams struct {
	order  []string
	values map[string]*yaml.Node
}

func parseGo2RTCStreams(content []byte) (go2RTCStreams, []Finding) {
	result := go2RTCStreams{values: map[string]*yaml.Node{}}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return result, []Finding{{Code: "GO2RTC_YAML_INVALID", Message: "the go2rtc source is not valid YAML"}}
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return result, []Finding{{Code: "GO2RTC_ROOT_INVALID", Message: "the go2rtc source root must be a mapping"}}
	}
	root := document.Content[0]
	var streamsNode *yaml.Node
	streamsCount := 0
	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value == "streams" {
			streamsCount++
			streamsNode = root.Content[index+1]
		}
	}
	if streamsCount != 1 || streamsNode == nil || streamsNode.Kind != yaml.MappingNode {
		return result, []Finding{{Code: "GO2RTC_STREAMS_INVALID", Message: "the go2rtc source must contain exactly one streams mapping"}}
	}
	var blockers []Finding
	for index := 0; index+1 < len(streamsNode.Content); index += 2 {
		keyNode, valueNode := streamsNode.Content[index], streamsNode.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" || strings.TrimSpace(keyNode.Value) == "" {
			blockers = append(blockers, Finding{Code: "GO2RTC_STREAM_KEY_INVALID", Message: "a go2rtc stream key is not a nonempty string"})
			continue
		}
		key := keyNode.Value
		if _, exists := result.values[key]; exists {
			blockers = append(blockers, Finding{Code: "GO2RTC_STREAM_KEY_DUPLICATE", Message: "the go2rtc streams mapping contains a duplicate key"})
			continue
		}
		result.order = append(result.order, key)
		result.values[key] = valueNode
	}
	return result, blockers
}

func validatedGo2RTCProducer(node *yaml.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	var raw string
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return "", false
		}
		raw = node.Value
	case yaml.SequenceNode:
		if len(node.Content) != 1 || node.Content[0].Kind != yaml.ScalarNode || node.Content[0].Tag != "!!str" {
			return "", false
		}
		raw = node.Content[0].Value
	default:
		return "", false
	}
	if raw == "" || raw != strings.TrimSpace(raw) || strings.HasPrefix(strings.ToLower(raw), "ffmpeg:") {
		return "", false
	}
	parsed, ok := parseStreamURL(raw)
	if !ok {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast())) {
		return "", false
	}
	return raw, true
}

func go2RTCCanaryCamera(key, mainURL, subURL string) store.Camera {
	parsed, _ := parseStreamURL(mainURL)
	rtspPort, httpPort := streamPorts(parsed)
	width, height := 1920, 1080
	return store.Camera{
		Name: key, URL: mainURL, StreamName: key, LayoutKey: key, State: "unknown", Enabled: true,
		Host: parsed.Hostname(), RTSPPort: rtspPort, HTTPPort: httpPort,
		Streams: []store.CameraStream{
			{Role: store.CameraStreamRoleRecording, SourceKey: "recording", Label: "recording", Source: "legacy-go2rtc", URL: mainURL, Go2RTCStreamName: key + "-input-recording", State: "unknown"},
			{Role: store.CameraStreamRoleLive, SourceKey: "live", Label: "live", Source: "legacy-go2rtc", URL: subURL, Go2RTCStreamName: key + "-input-live", State: "unknown"},
		},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputLive, SourceKey: "live", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationAlways},
			{Purpose: store.CameraOutputFocus, SourceKey: "recording", VideoMode: store.CameraVideoAuto, MaxWidth: &width, MaxHeight: &height, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
		LastProbeJSON: map[string]any{}, LastScanJSON: map[string]any{},
	}
}

func go2RTCCanaryLayout(keys []string) store.LayoutProfile {
	items := make([]store.LayoutItem, 0, len(keys))
	for index, key := range keys {
		item := store.LayoutItem{I: key, MinW: 8, MinH: 8}
		if index == 0 {
			item.X, item.Y, item.W, item.H = 0, 0, 24, 24
		} else {
			item.X, item.Y, item.W, item.H = 24+((index-1)%2)*12, ((index-1)/2)*12, 12, 12
		}
		items = append(items, item)
	}
	rows := 48
	return store.LayoutProfile{ID: "go2rtc-canary", Name: "집 카메라 canary", Data: items, GridCols: 48, GridRows: &rows}
}

func go2RTCCanarySettings(options Go2RTCCanaryOptions) store.SettingsUpdate {
	recording := store.RecordingSettings{
		SegmentMinutes: options.SegmentMinutes, RetentionDays: options.RetentionDays, MaxStorageGB: options.MaxStorageGB,
	}
	backup := store.BackupSettings{
		Enabled: false, Target: "", RetentionDays: 1, ScheduleEnabled: false, ScheduleCron: "0 3 * * *", ProtectUnbacked: true,
	}
	empty := ""
	alerts := store.AlertSettingsUpdate{DiscordEnabled: false, DiscordWebhookURL: &empty}
	return store.SettingsUpdate{Recording: &recording, Backup: &backup, Alerts: &alerts}
}
