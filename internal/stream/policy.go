package stream

import (
	"bytes"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"camstation/internal/store"
)

const privateSourcePrefix = "__camstation_source_"

type resolvedOutput struct {
	SourceName  string
	Producer    string
	Transcoding bool
	Result      store.CameraOutputApplyResult
}

func resolveOutput(camera store.Camera, output store.CameraOutput) (resolvedOutput, error) {
	return resolveOutputWithEffective(camera, output, nil)
}

func resolveOutputWithEffective(camera store.Camera, output store.CameraOutput, applied *store.CameraOutputVerification) (resolvedOutput, error) {
	source, ok := sourceForOutput(camera, output)
	if !ok {
		return resolvedOutput{}, fmt.Errorf("output %s source %q not found", output.Purpose, output.SourceKey)
	}
	width, height := boundedDimensions(source.DetectedWidth, source.DetectedHeight, output.MaxWidth, output.MaxHeight)
	fps := source.DetectedFPS
	if output.MaxFPS != nil && (fps <= 0 || fps > *output.MaxFPS) {
		fps = *output.MaxFPS
	}
	resizeRequired := output.MaxWidth != nil && output.MaxHeight != nil &&
		(source.DetectedWidth <= 0 || source.DetectedHeight <= 0 || width != source.DetectedWidth || height != source.DetectedHeight)
	fpsRequired := output.MaxFPS != nil && (source.DetectedFPS <= 0 || source.DetectedFPS > *output.MaxFPS)
	transcode := output.VideoMode == store.CameraVideoH264 ||
		(output.VideoMode == store.CameraVideoAuto && (!browserSafeH264(source) || resizeRequired || fpsRequired))
	if applied != nil {
		transcode = applied.Transcoding
		width, height, fps = applied.Width, applied.Height, applied.FPS
	}

	video := "copy"
	videoCodec := source.DetectedVideoCodec
	if transcode {
		video = "h264"
		videoCodec = "h264"
	}
	sourceName := PrivateSourceName(camera.ID, source.ID)
	producer := "rtsp://127.0.0.1:8554/" + sourceName
	if transcode || output.AudioMode != store.CameraAudioSource {
		parts := []string{"ffmpeg:" + sourceName, "video=" + video}
		var raw []string
		if transcode && output.MaxWidth != nil && output.MaxHeight != nil && (resizeRequired || applied != nil) {
			capWidth, capHeight := *output.MaxWidth, *output.MaxHeight
			if applied != nil && applied.Width > 0 && applied.Height > 0 {
				capWidth, capHeight = applied.Width, applied.Height
			}
			raw = append(raw, fmt.Sprintf("-vf scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease:force_divisible_by=2", capWidth, capHeight))
		}
		if transcode && (fpsRequired || (applied != nil && output.MaxFPS != nil && applied.FPS > 0)) {
			raw = append(raw, "-r "+formatFPS(fps))
		}
		if len(raw) > 0 {
			parts = append(parts, "raw="+strings.Join(raw, " "))
		}
		switch output.AudioMode {
		case store.CameraAudioSource:
			parts = append(parts, "audio=copy")
		case store.CameraAudioAAC:
			parts = append(parts, "audio=aac")
		}
		producer = strings.Join(parts, "#")
	}
	verification := store.CameraOutputVerification{
		VideoCodec:  videoCodec,
		AudioCodec:  source.DetectedAudioCodec,
		Width:       width,
		Height:      height,
		FPS:         fps,
		Transcoding: transcode,
		Error:       source.DetectedError,
	}
	if output.AudioMode == store.CameraAudioNone {
		verification.AudioCodec = ""
	} else if output.AudioMode == store.CameraAudioAAC {
		verification.AudioCodec = "aac"
	}
	if applied != nil {
		verification = *applied
	}
	return resolvedOutput{
		SourceName:  sourceName,
		Producer:    producer,
		Transcoding: transcode,
		Result: store.CameraOutputApplyResult{
			Purpose: output.Purpose,
			Policy: store.CameraOutputPolicySnapshot{
				SourceStreamID: output.SourceStreamID,
				SourceKey:      source.SourceKey,
				VideoMode:      output.VideoMode,
				MaxWidth:       output.MaxWidth,
				MaxHeight:      output.MaxHeight,
				MaxFPS:         output.MaxFPS,
				AudioMode:      output.AudioMode,
				Activation:     output.Activation,
			},
			Verification: verification,
		},
	}, nil
}

func renderPolicyConfig(cameras []store.Camera, applied bool) ([]byte, map[int64][]store.CameraOutputApplyResult, error) {
	resolved := make(map[int64][]resolvedOutput, len(cameras))
	results := make(map[int64][]store.CameraOutputApplyResult, len(cameras))
	for _, camera := range cameras {
		for _, output := range camera.Outputs {
			var effective *store.CameraOutputVerification
			if applied && output.AppliedPolicy.SourceKey != "" {
				effective = &output.Verification
				output = outputFromSnapshot(output, output.AppliedPolicy)
			} else if applied && camera.PolicyState.AppliedRevision > 0 {
				return nil, nil, fmt.Errorf("camera %s output %s is missing applied snapshot", camera.StreamName, output.Purpose)
			}
			item, err := resolveOutputWithEffective(camera, output, effective)
			if err != nil {
				return nil, nil, err
			}
			resolved[camera.ID] = append(resolved[camera.ID], item)
			results[camera.ID] = append(results[camera.ID], item.Result)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("api:\n  listen: 127.0.0.1:1984\n")
	buf.WriteString("rtsp:\n  listen: 127.0.0.1:8554\n")
	buf.WriteString("webrtc:\n  listen: 0.0.0.0:8555\n")
	if candidates := localCandidates(8555); len(candidates) > 0 {
		buf.WriteString("  candidates:\n")
		for _, candidate := range candidates {
			fmt.Fprintf(&buf, "    - %s\n", quoteYAML(candidate))
		}
	}
	buf.WriteString("ffmpeg:\n")
	buf.WriteString("  h264: \"-codec:v libx264 -preset:v veryfast -tune:v zerolatency -pix_fmt:v yuv420p -g 20 -keyint_min 20 -sc_threshold 0\"\n")
	preload := false
	preloaded := make(map[string]bool)
	writePreload := func(name, tracks string) {
		if preloaded[name] {
			return
		}
		if !preload {
			buf.WriteString("preload:\n")
			preload = true
		}
		fmt.Fprintf(&buf, "  %s: %s\n", quoteYAML(name), quoteYAML(tracks))
		preloaded[name] = true
	}
	for _, camera := range cameras {
		for i, output := range camera.Outputs {
			if applied && output.AppliedPolicy.SourceKey != "" {
				output = outputFromSnapshot(output, output.AppliedPolicy)
			}
			if i >= len(resolved[camera.ID]) {
				continue
			}
			if output.Purpose == store.CameraOutputLive {
				writePreload(resolved[camera.ID][i].SourceName, "video&audio")
			}
			if output.Activation != store.CameraActivationAlways {
				continue
			}
			tracks := "video&audio"
			if output.AudioMode == store.CameraAudioNone {
				tracks = "video"
			}
			writePreload(output.StreamName, tracks)
		}
	}
	buf.WriteString("streams:\n")
	wrote := false
	for _, camera := range cameras {
		usedSources := map[string]bool{}
		for _, output := range resolved[camera.ID] {
			usedSources[output.SourceName] = true
		}
		for _, source := range camera.Streams {
			name := PrivateSourceName(camera.ID, source.ID)
			if source.URL == "" || !usedSources[name] {
				continue
			}
			fmt.Fprintf(&buf, "  %s:\n    - %s\n", quoteYAML(name), quoteYAML(privateInputProducer(source.URL)))
			wrote = true
		}
		for i, output := range camera.Outputs {
			if i >= len(resolved[camera.ID]) {
				continue
			}
			fmt.Fprintf(&buf, "  %s:\n    - %s\n", quoteYAML(output.StreamName), quoteYAML(resolved[camera.ID][i].Producer))
			wrote = true
		}
	}
	if !wrote {
		buf.WriteString("  {}\n")
	}
	return buf.Bytes(), results, nil
}

func privateInputProducer(rawURL string) string {
	producer := "ffmpeg:" + rawURL + "#video=copy#audio=copy"
	parsed, err := url.Parse(rawURL)
	if err == nil && (strings.EqualFold(parsed.Scheme, "rtsp") || strings.EqualFold(parsed.Scheme, "rtsps")) {
		producer += "#timeout=5"
	}
	return producer
}

func renderStartupConfig(cameras []store.Camera) ([]byte, error) {
	config, _, err := renderPolicyConfig(cameras, true)
	return config, err
}

func sourceForOutput(camera store.Camera, output store.CameraOutput) (store.CameraStream, bool) {
	for _, source := range camera.Streams {
		if output.SourceKey != "" && source.SourceKey == output.SourceKey {
			return source, true
		}
	}
	if output.SourceKey == "" {
		for _, source := range camera.Streams {
			if output.SourceStreamID != 0 && source.ID == output.SourceStreamID {
				return source, true
			}
		}
	}
	return store.CameraStream{}, false
}

func outputFromSnapshot(output store.CameraOutput, policy store.CameraOutputPolicySnapshot) store.CameraOutput {
	output.SourceStreamID = policy.SourceStreamID
	output.SourceKey = policy.SourceKey
	output.VideoMode = policy.VideoMode
	output.MaxWidth = policy.MaxWidth
	output.MaxHeight = policy.MaxHeight
	output.MaxFPS = policy.MaxFPS
	output.AudioMode = policy.AudioMode
	output.Activation = policy.Activation
	return output
}

func browserSafeH264(source store.CameraStream) bool {
	codec := strings.ToLower(source.DetectedVideoCodec)
	pixelFormat := strings.ToLower(source.DetectedPixelFormat)
	profile := strings.ToLower(strings.TrimSpace(source.DetectedProfile))
	level, err := strconv.ParseFloat(strings.TrimSpace(source.DetectedLevel), 64)
	if err == nil && level > 10 {
		level /= 10
	}
	profileSafe := profile == "baseline" || profile == "constrained baseline" || profile == "main" || profile == "high"
	return !source.DetectedCheckedAt.IsZero() && source.DetectedError == "" &&
		(codec == "h264" || codec == "avc" || codec == "avc1") &&
		source.DetectedBitDepth == 8 && (pixelFormat == "yuv420p" || pixelFormat == "yuvj420p") &&
		profileSafe && err == nil && level >= 1 && level <= 5.2
}

func boundedDimensions(width, height int, maxWidth, maxHeight *int) (int, int) {
	if maxWidth == nil || maxHeight == nil {
		return width, height
	}
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	scale := math.Min(1, math.Min(float64(*maxWidth)/float64(width), float64(*maxHeight)/float64(height)))
	return evenFloor(float64(width) * scale), evenFloor(float64(height) * scale)
}

func evenFloor(value float64) int {
	result := int(math.Floor(value)) &^ 1
	if result < 2 {
		return 2
	}
	return result
}

func formatFPS(fps float64) string {
	return strconv.FormatFloat(fps, 'f', -1, 64)
}

// PrivateSourceName returns the canonical server-internal go2rtc input alias.
func PrivateSourceName(cameraID, sourceID int64) string {
	return fmt.Sprintf("%s%d_%d", privateSourcePrefix, cameraID, sourceID)
}
