package legacyimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"camstation/internal/store"
)

func buildPlan(ctx context.Context, source string, expectations Expectations) (plan, error) {
	result := plan{manifest: Manifest{FormatVersion: ManifestVersion}}
	db, err := openLegacyReadOnly(ctx, source)
	if err != nil {
		return result, err
	}
	defer db.Close()

	if !inspectLegacySchema(ctx, db, &result.manifest) {
		result.manifest.Ready = false
		return result, nil
	}

	legacyCameras, archivedCount, err := readLegacyCameras(ctx, db)
	if err != nil {
		return result, err
	}
	if len(legacyCameras) == 0 {
		result.manifest.Blockers = append(result.manifest.Blockers, Finding{Code: "CAMERAS_EMPTY", Message: "the active 1.x import set contains no cameras"})
	}
	legacyLayouts, err := readLegacyLayouts(ctx, db)
	if err != nil {
		return result, err
	}
	legacySettings, err := readLegacySettings(ctx, db)
	if err != nil {
		return result, err
	}

	result.manifest.Summary.ArchivedCount = archivedCount
	if archivedCount > 0 {
		result.manifest.Warnings = append(result.manifest.Warnings, Finding{
			Code:    "ARCHIVED_CAMERAS_EXCLUDED",
			Message: fmt.Sprintf("%d archived camera row(s) remain only in the preserved 1.x snapshot", archivedCount),
		})
	}

	keys := map[string]bool{}
	for _, legacy := range legacyCameras {
		camera, public, canonical, blockers, warnings := convertCamera(legacy)
		result.manifest.Blockers = append(result.manifest.Blockers, blockers...)
		result.manifest.Warnings = append(result.manifest.Warnings, warnings...)
		if keys[legacy.id] {
			result.manifest.Blockers = append(result.manifest.Blockers, Finding{Code: "CAMERA_KEY_DUPLICATE", Message: "camera key is duplicated: " + legacy.id})
		}
		keys[legacy.id] = true
		result.cameras = append(result.cameras, plannedCamera{row: camera})
		result.manifest.Cameras = append(result.manifest.Cameras, public)
		result.canon.Cameras = append(result.canon.Cameras, canonical)
		result.manifest.Summary.CameraCount++
		if legacy.enabled {
			result.manifest.Summary.EnabledCount++
		} else {
			result.manifest.Summary.DisabledCount++
		}
		if public.HasSubStream {
			result.manifest.Summary.SubStreamCount++
		}
		if public.ControlCredentialsDeferred {
			result.manifest.Summary.DeferredControl++
		}
		if public.LocationPresent {
			result.manifest.Summary.UnmappedMetadata++
		}
		if public.NotesPresent {
			result.manifest.Summary.UnmappedMetadata++
		}
	}

	for _, legacy := range legacyLayouts {
		layout, public, blockers := convertLayout(legacy, keys)
		result.layouts = append(result.layouts, layout)
		result.manifest.Layouts = append(result.manifest.Layouts, public)
		result.manifest.Blockers = append(result.manifest.Blockers, blockers...)
		result.canon.Layouts = append(result.canon.Layouts, canonicalLayoutFromStore(layout))
		result.manifest.Summary.LayoutCount++
		result.manifest.Summary.LayoutItemCount += len(layout.Data)
	}

	result.settings = convertSettings(legacySettings, &result.manifest)
	result.canon.Settings = canonicalSettings{
		Recording: *result.settings.Recording,
		Backup:    *result.settings.Backup,
	}
	validateExpectations(&result.manifest, expectations)
	sortFindings(result.manifest.Warnings)
	sortFindings(result.manifest.Blockers)
	result.manifest.Ready = result.manifest.SchemaCompatible && result.manifest.QuickCheck == "ok" && len(result.manifest.Blockers) == 0
	if result.manifest.Ready {
		result.manifest.CanonicalFingerprint = canonicalFingerprint(result.canon)
	}
	return result, nil
}

func convertCamera(legacy legacyCamera) (store.Camera, CameraManifest, canonicalCamera, []Finding, []Finding) {
	key := strings.TrimSpace(legacy.id)
	name := strings.TrimSpace(legacy.displayName)
	mainURL := strings.TrimSpace(legacy.mainURL)
	subURL := strings.TrimSpace(legacy.subURL.String)
	derivedLive, derivedLiveOK := parseDerivedLiveRecipe(subURL, key)
	public := CameraManifest{
		Key:                key,
		Name:               name,
		Enabled:            legacy.enabled,
		HasSubStream:       subURL != "",
		MainURLFingerprint: secretFingerprint(mainURL),
		LocationPresent:    strings.TrimSpace(legacy.location.String) != "",
		NotesPresent:       strings.TrimSpace(legacy.notes.String) != "",
		SortOrder:          legacy.sortOrder,
	}
	if subURL != "" {
		public.SubURLFingerprint = secretFingerprint(subURL)
	}

	var blockers, warnings []Finding
	if key != legacy.id || !validLegacyCameraKey(key) {
		blockers = append(blockers, Finding{Code: "CAMERA_KEY_INVALID", Message: "camera key is unsafe or would change during import"})
	}
	if name == "" {
		blockers = append(blockers, Finding{Code: "CAMERA_NAME_INVALID", Message: "camera " + key + " has an empty display name"})
	}
	parsedMain, mainOK := parseStreamURL(mainURL)
	if !mainOK || mainURL != legacy.mainURL {
		blockers = append(blockers, Finding{Code: "CAMERA_MAIN_URL_INVALID", Message: "camera " + key + " has an invalid recording stream URL"})
	}
	if subURL != "" {
		if _, ok := parseStreamURL(subURL); (!ok && !derivedLiveOK) || subURL != legacy.subURL.String {
			blockers = append(blockers, Finding{Code: "CAMERA_SUB_URL_INVALID", Message: "camera " + key + " has an invalid live stream URL"})
		}
		if derivedLiveOK {
			warnings = append(warnings, Finding{
				Code:    "CAMERA_DERIVED_LIVE_MAPPED",
				Message: "camera " + key + " maps its legacy local ffmpeg recipe to a 2.0 H.264 live output",
			})
		}
	}

	host, rtspPort, httpPort := "", 0, 0
	if parsedMain != nil {
		host = parsedMain.Hostname()
		rtspPort, httpPort = streamPorts(parsedMain)
	}
	onvifFieldsPresent := strings.TrimSpace(legacy.onvifHost.String) != "" || legacy.onvifPort.Valid ||
		strings.TrimSpace(legacy.onvifUsername.String) != "" || strings.TrimSpace(legacy.onvifPassword.String) != ""
	onvifConfigured := validHost(legacy.onvifHost.String) && legacy.onvifPort.Valid && legacy.onvifPort.Int64 > 0 && legacy.onvifPort.Int64 <= 65535
	public.ONVIFConfigured = onvifConfigured
	onvifPort := 0
	if onvifConfigured && onvifCredentialsMatch(parsedMain, legacy) {
		host = strings.TrimSpace(legacy.onvifHost.String)
		onvifPort = int(legacy.onvifPort.Int64)
	} else if onvifFieldsPresent {
		public.ControlCredentialsDeferred = true
		warnings = append(warnings, Finding{
			Code:    "CAMERA_CONTROL_DEFERRED",
			Message: "camera " + key + " retains video but requires a separate post-cutover ONVIF control review",
		})
	}
	if public.LocationPresent || public.NotesPresent {
		warnings = append(warnings, Finding{
			Code:    "CAMERA_METADATA_SOURCE_ONLY",
			Message: "camera " + key + " location or notes remain in the preserved root-only 1.x snapshot",
		})
	}

	inputs := []store.CameraStream{{
		Role:             store.CameraStreamRoleRecording,
		SourceKey:        "recording",
		Label:            "recording",
		Source:           "legacy-1x",
		URL:              mainURL,
		Go2RTCStreamName: key + "-input-recording",
		State:            "unknown",
	}}
	liveSource := "recording"
	if subURL != "" && !derivedLiveOK {
		inputs = append(inputs, store.CameraStream{
			Role:             store.CameraStreamRoleLive,
			SourceKey:        "live",
			Label:            "live",
			Source:           "legacy-1x",
			URL:              subURL,
			Go2RTCStreamName: key + "-input-live",
			State:            "unknown",
		})
		liveSource = "live"
	}
	width, height := 1920, 1080
	liveVideoMode := store.CameraVideoAuto
	var liveMaxWidth, liveMaxHeight *int
	if derivedLiveOK {
		liveVideoMode = store.CameraVideoH264
		liveMaxWidth, liveMaxHeight = derivedLive.width, derivedLive.height
	}
	outputs := []store.CameraOutput{
		{Purpose: store.CameraOutputRecording, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
		{Purpose: store.CameraOutputLive, SourceKey: liveSource, VideoMode: liveVideoMode, MaxWidth: liveMaxWidth, MaxHeight: liveMaxHeight, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		{Purpose: store.CameraOutputFocus, SourceKey: "recording", VideoMode: store.CameraVideoAuto, MaxWidth: &width, MaxHeight: &height, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
	}
	row := store.Camera{
		Name:          name,
		URL:           mainURL,
		StreamName:    key,
		LayoutKey:     key,
		State:         "unknown",
		Enabled:       legacy.enabled,
		Host:          host,
		RTSPPort:      rtspPort,
		HTTPPort:      httpPort,
		ONVIFPort:     onvifPort,
		Streams:       inputs,
		Outputs:       outputs,
		LastProbeJSON: map[string]any{},
		LastScanJSON:  map[string]any{},
	}
	canonical := canonicalCameraFromStore(row)
	return row, public, canonical, blockers, warnings
}

type derivedLiveRecipe struct {
	width  *int
	height *int
}

func parseDerivedLiveRecipe(raw, cameraKey string) (derivedLiveRecipe, bool) {
	if !strings.HasPrefix(raw, "ffmpeg:") {
		return derivedLiveRecipe{}, false
	}
	parts := strings.Split(strings.TrimPrefix(raw, "ffmpeg:"), "#")
	if len(parts) < 2 {
		return derivedLiveRecipe{}, false
	}
	parsed, ok := parseStreamURL(parts[0])
	if !ok || !strings.EqualFold(parsed.Scheme, "rtsp") || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery {
		return derivedLiveRecipe{}, false
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return derivedLiveRecipe{}, false
	}
	port, httpPort := streamPorts(parsed)
	if port != 8554 || httpPort != 0 || strings.Trim(parsed.Path, "/") != cameraKey {
		return derivedLiveRecipe{}, false
	}

	options := make(map[string]string, len(parts)-1)
	for _, rawOption := range parts[1:] {
		key, value, found := strings.Cut(rawOption, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if !found || key == "" || value == "" || options[key] != "" {
			return derivedLiveRecipe{}, false
		}
		switch key {
		case "video", "width", "height":
			options[key] = value
		default:
			return derivedLiveRecipe{}, false
		}
	}
	if !strings.EqualFold(options["video"], "h264") {
		return derivedLiveRecipe{}, false
	}
	if (options["width"] == "") != (options["height"] == "") {
		return derivedLiveRecipe{}, false
	}
	recipe := derivedLiveRecipe{}
	if options["width"] != "" {
		width, widthErr := strconv.Atoi(options["width"])
		height, heightErr := strconv.Atoi(options["height"])
		if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 || width > 8192 || height > 8192 || width%2 != 0 || height%2 != 0 {
			return derivedLiveRecipe{}, false
		}
		recipe.width, recipe.height = &width, &height
	}
	return recipe, true
}

func parseStreamURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Fragment != "" || !validURLPort(parsed) {
		return nil, false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "rtsp", "rtsps", "http", "https":
		return parsed, true
	default:
		return nil, false
	}
}

func validURLPort(parsed *url.URL) bool {
	if parsed.Port() == "" {
		return true
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port > 0 && port <= 65535
}

func streamPorts(parsed *url.URL) (int, int) {
	if parsed == nil {
		return 0, 0
	}
	port := 0
	if parsed.Port() != "" {
		port, _ = strconv.Atoi(parsed.Port())
	}
	switch strings.ToLower(parsed.Scheme) {
	case "rtsp", "rtsps":
		if port == 0 {
			port = 554
		}
		return port, 0
	case "http":
		if port == 0 {
			port = 80
		}
		return 0, port
	case "https":
		if port == 0 {
			port = 443
		}
		return 0, port
	default:
		return 0, 0
	}
}

func validHost(raw string) bool {
	host := strings.TrimSpace(raw)
	if host == "" || host != raw || len(host) > 253 || strings.ContainsAny(host, " /\\?#\t\r\n") {
		return false
	}
	return !strings.Contains(host, ":") || net.ParseIP(host) != nil
}

func onvifCredentialsMatch(parsed *url.URL, camera legacyCamera) bool {
	username := strings.TrimSpace(camera.onvifUsername.String)
	password := strings.TrimSpace(camera.onvifPassword.String)
	if username == "" && password == "" {
		return true
	}
	if parsed == nil || parsed.User == nil {
		return false
	}
	parsedPassword, _ := parsed.User.Password()
	return parsed.User.Username() == username && parsedPassword == password
}

func convertLayout(legacy legacyLayout, cameraKeys map[string]bool) (store.LayoutProfile, LayoutManifest, []Finding) {
	public := LayoutManifest{ID: legacy.id, Name: strings.TrimSpace(legacy.name), SourceGridCols: legacy.gridCols, TargetGridCols: 48}
	var blockers []Finding
	items, err := decodeLayoutItems(legacy.data)
	if err != nil {
		blockers = append(blockers, Finding{Code: "LAYOUT_JSON_INVALID", Message: "layout " + legacy.id + " data is not valid JSON"})
		items = []store.LayoutItem{}
	}
	factor := 0
	if legacy.gridCols > 0 && 48%legacy.gridCols == 0 {
		factor = 48 / legacy.gridCols
	} else {
		blockers = append(blockers, Finding{Code: "LAYOUT_GRID_UNSUPPORTED", Message: "layout " + legacy.id + " cannot be scaled exactly to 48 columns"})
	}
	seen := map[string]bool{}
	rowLimit := 48
	if legacy.gridRows.Valid && legacy.gridRows.Int64 > 0 && legacy.gridRows.Int64 <= 48 {
		rowLimit = int(legacy.gridRows.Int64)
	}
	for index := range items {
		item := &items[index]
		if factor > 0 {
			item.X *= factor
			item.W *= factor
			item.MinW *= factor
		}
		if seen[item.I] {
			blockers = append(blockers, Finding{Code: "LAYOUT_KEY_DUPLICATE", Message: "layout " + legacy.id + " contains a duplicate camera key"})
		}
		seen[item.I] = true
		if !cameraKeys[item.I] {
			blockers = append(blockers, Finding{Code: "LAYOUT_CAMERA_MISSING", Message: "layout " + legacy.id + " references a camera not present in the active import set"})
		}
		if item.X < 0 || item.Y < 0 || item.W <= 0 || item.H <= 0 || item.X+item.W > 48 || item.Y+item.H > rowLimit {
			blockers = append(blockers, Finding{Code: "LAYOUT_ITEM_OUT_OF_BOUNDS", Message: "layout " + legacy.id + " contains an out-of-bounds item"})
		}
		if item.MinW < 0 || item.MinH < 0 || item.MinW > 48 || item.MinH > rowLimit {
			blockers = append(blockers, Finding{Code: "LAYOUT_ITEM_MINIMUM_INVALID", Message: "layout " + legacy.id + " contains an invalid minimum size"})
		}
	}
	for left := 0; left < len(items); left++ {
		for right := left + 1; right < len(items); right++ {
			if layoutItemsOverlap(items[left], items[right]) {
				blockers = append(blockers, Finding{Code: "LAYOUT_ITEMS_OVERLAP", Message: "layout " + legacy.id + " contains overlapping items"})
			}
		}
	}
	gridRows := (*int)(nil)
	if legacy.gridRows.Valid {
		value := int(legacy.gridRows.Int64)
		gridRows = &value
		if value <= 0 || value > 48 {
			blockers = append(blockers, Finding{Code: "LAYOUT_ROWS_UNSUPPORTED", Message: "layout " + legacy.id + " has an unsupported row count"})
		}
	}
	layout := store.LayoutProfile{
		ID:                legacy.id,
		Name:              public.Name,
		Data:              items,
		TimelineCollapsed: legacy.timelineCollapsed,
		GridCols:          48,
		GridRows:          gridRows,
	}
	if !validLegacyCameraKey(layout.ID) || public.Name == "" {
		blockers = append(blockers, Finding{Code: "LAYOUT_IDENTITY_INVALID", Message: "layout id and name must be nonempty"})
	}
	public.ItemCount = len(items)
	public.CanonicalFingerprint = canonicalFingerprint(canonicalLayoutFromStore(layout))
	return layout, public, blockers
}

func layoutItemsOverlap(left, right store.LayoutItem) bool {
	return left.X < right.X+right.W && left.X+left.W > right.X && left.Y < right.Y+right.H && left.Y+left.H > right.Y
}

func convertSettings(values map[string]string, manifest *Manifest) store.SettingsUpdate {
	segment := parseLegacyInt(values, "segment_minutes", 30, manifest)
	retention := parseLegacyInt(values, "retention_days", 30, manifest)
	maxStorage := parseLegacyFloat(values, "max_storage_gb", 0, manifest)
	motionEnabled := parseLegacyBool(values, "motion_enabled", false, manifest)
	motionThreshold := parseLegacyFloat(values, "motion_threshold", 0.02, manifest)
	if segment <= 0 || retention < 0 || maxStorage < 0 {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "RECORDING_SETTINGS_INVALID", Message: "legacy recording settings are outside supported bounds"})
	}
	recording := store.RecordingSettings{SegmentMinutes: segment, RetentionDays: retention, MaxStorageGB: maxStorage}
	backup := store.BackupSettings{
		Enabled:         false,
		Target:          "",
		RetentionDays:   30,
		ScheduleEnabled: false,
		ScheduleCron:    "0 3 * * *",
		ProtectUnbacked: true,
	}
	manifest.Settings = SettingsManifest{
		SegmentMinutes:       segment,
		RetentionDays:        retention,
		MaxStorageGB:         maxStorage,
		MotionEnabled:        motionEnabled,
		MotionThreshold:      motionThreshold,
		BackupEnabled:        false,
		BackupTargetPresent:  false,
		ProtectUnbacked:      true,
		MotionSettingsMapped: false,
	}
	if motionEnabled {
		manifest.Warnings = append(manifest.Warnings, Finding{Code: "MOTION_SETTINGS_NOT_MAPPED", Message: "legacy motion processing is not part of the approved video-only 2.0 cutover"})
	}
	return store.SettingsUpdate{Recording: &recording, Backup: &backup}
}

func validateExpectations(manifest *Manifest, expected Expectations) {
	if expected.CameraCount > 0 && manifest.Summary.CameraCount != expected.CameraCount {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_CAMERA_COUNT_MISMATCH", Message: fmt.Sprintf("camera count is %d, expected %d", manifest.Summary.CameraCount, expected.CameraCount)})
	}
	if expected.EnabledCount > 0 && manifest.Summary.EnabledCount != expected.EnabledCount {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_ENABLED_COUNT_MISMATCH", Message: fmt.Sprintf("enabled camera count is %d, expected %d", manifest.Summary.EnabledCount, expected.EnabledCount)})
	}
	if expected.SubStreamCount > 0 && manifest.Summary.SubStreamCount != expected.SubStreamCount {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_SUB_STREAM_COUNT_MISMATCH", Message: fmt.Sprintf("sub-stream count is %d, expected %d", manifest.Summary.SubStreamCount, expected.SubStreamCount)})
	}
	if expected.DisabledCamera != "" {
		found := false
		for _, camera := range manifest.Cameras {
			if camera.Key == expected.DisabledCamera {
				found = true
				if camera.Enabled {
					manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_DISABLED_CAMERA_ENABLED", Message: "expected disabled camera is enabled: " + expected.DisabledCamera})
				}
			}
		}
		if !found {
			manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_DISABLED_CAMERA_MISSING", Message: "expected disabled camera is missing: " + expected.DisabledCamera})
		}
	}
	if expected.LayoutCount > 0 && manifest.Summary.LayoutCount != expected.LayoutCount {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_LAYOUT_COUNT_MISMATCH", Message: fmt.Sprintf("layout count is %d, expected %d", manifest.Summary.LayoutCount, expected.LayoutCount)})
	}
	if expected.LayoutItemCount > 0 && manifest.Summary.LayoutItemCount != expected.LayoutItemCount {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_LAYOUT_ITEM_COUNT_MISMATCH", Message: fmt.Sprintf("layout item count is %d, expected %d", manifest.Summary.LayoutItemCount, expected.LayoutItemCount)})
	}
	if expected.SegmentMinutes > 0 && manifest.Settings.SegmentMinutes != expected.SegmentMinutes {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_SEGMENT_MINUTES_MISMATCH", Message: fmt.Sprintf("segment minutes is %d, expected %d", manifest.Settings.SegmentMinutes, expected.SegmentMinutes)})
	}
	if expected.RetentionDays > 0 && manifest.Settings.RetentionDays != expected.RetentionDays {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_RETENTION_DAYS_MISMATCH", Message: fmt.Sprintf("retention days is %d, expected %d", manifest.Settings.RetentionDays, expected.RetentionDays)})
	}
	if expected.MaxStorageGB > 0 && manifest.Settings.MaxStorageGB != expected.MaxStorageGB {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "EXPECTED_MAX_STORAGE_MISMATCH", Message: fmt.Sprintf("max storage GB is %g, expected %g", manifest.Settings.MaxStorageGB, expected.MaxStorageGB)})
	}
}

func canonicalCameraFromStore(camera store.Camera) canonicalCamera {
	inputs := make([]canonicalCameraInput, 0, len(camera.Streams))
	for _, input := range camera.Streams {
		inputs = append(inputs, canonicalCameraInput{
			Role: input.Role, SourceKey: input.SourceKey, Label: input.Label, Source: input.Source,
			URL: input.URL, Go2RTCStreamName: input.Go2RTCStreamName,
		})
	}
	outputs := make([]canonicalCameraOutput, 0, len(camera.Outputs))
	for _, output := range camera.Outputs {
		streamName := output.StreamName
		if streamName == "" {
			streamName = camera.StreamName + "-" + string(output.Purpose)
		}
		outputs = append(outputs, canonicalCameraOutput{
			Purpose: output.Purpose, StreamName: streamName, SourceKey: output.SourceKey, VideoMode: output.VideoMode,
			MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS,
			AudioMode: output.AudioMode, Activation: output.Activation,
		})
	}
	desiredRevision := camera.PolicyState.DesiredRevision
	applyState := camera.PolicyState.ApplyState
	if desiredRevision == 0 && applyState == "" {
		desiredRevision = 1
		applyState = store.CameraApplyPending
	}
	return canonicalCamera{
		Key: camera.StreamName, Name: camera.Name, Enabled: camera.Enabled, LayoutKey: camera.LayoutKey,
		Host: camera.Host, RTSPPort: camera.RTSPPort, HTTPPort: camera.HTTPPort, ONVIFPort: camera.ONVIFPort,
		DesiredRevision: desiredRevision, AppliedRevision: camera.PolicyState.AppliedRevision, ApplyState: applyState,
		Inputs: inputs, Outputs: outputs,
	}
}

func canonicalLayoutFromStore(layout store.LayoutProfile) canonicalLayout {
	return canonicalLayout{ID: layout.ID, Name: layout.Name, Data: layout.Data, TimelineCollapsed: layout.TimelineCollapsed, GridCols: layout.GridCols, GridRows: layout.GridRows}
}

func canonicalFingerprint(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func secretFingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Code == findings[j].Code {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Code < findings[j].Code
	})
}
