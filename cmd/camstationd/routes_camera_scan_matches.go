package main

import (
	"context"
	"strconv"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

func (d routeDeps) scanMatchResponse(ctx context.Context, scan cameraprofile.DeviceScanResult) (map[string]any, error) {
	templates, err := d.db.ListCameraProfileTemplates(ctx)
	if err != nil {
		return nil, err
	}
	result := cameraprofile.MatchProfileTemplates(scan, profileTemplateMatcherInputs(templates))
	return map[string]any{
		"ok":             true,
		"scan":           redactDeviceScanResult(scan),
		"matches":        publicProfileTemplateMatches(result.Matches),
		"recommendation": publicProfileTemplateMatchPointer(result.Recommendation),
	}, nil
}

type publicProfileTemplateMatch struct {
	TemplateID int64    `json:"templateId"`
	Name       string   `json:"name"`
	Confidence int      `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

func publicProfileTemplateMatches(matches []cameraprofile.ProfileTemplateMatch) []publicProfileTemplateMatch {
	out := make([]publicProfileTemplateMatch, 0, len(matches))
	for _, match := range matches {
		out = append(out, publicProfileTemplateMatchFromMatcher(match))
	}
	return out
}

func publicProfileTemplateMatchPointer(match *cameraprofile.ProfileTemplateMatch) *publicProfileTemplateMatch {
	if match == nil {
		return nil
	}
	public := publicProfileTemplateMatchFromMatcher(*match)
	return &public
}

func publicProfileTemplateMatchFromMatcher(match cameraprofile.ProfileTemplateMatch) publicProfileTemplateMatch {
	id, _ := strconv.ParseInt(match.TemplateID, 10, 64)
	return publicProfileTemplateMatch{
		TemplateID: id,
		Name:       match.Name,
		Confidence: match.Confidence,
		Reasons:    match.Reasons,
	}
}

func profileTemplateMatcherInputs(templates []store.CameraProfileTemplate) []cameraprofile.ProfileTemplateInput {
	inputs := make([]cameraprofile.ProfileTemplateInput, 0, len(templates))
	for _, template := range templates {
		input := cameraprofile.ProfileTemplateInput{
			ID:           strconv.FormatInt(template.ID, 10),
			Name:         template.ProfileName,
			Adapter:      template.Adapter,
			Manufacturer: template.Manufacturer,
			Model:        template.Model,
			Channels:     profileTemplateMatcherChannels(template.Channels),
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func profileTemplateMatcherChannels(channels []store.CameraProfileTemplateChannel) []cameraprofile.ProfileTemplateChannelInput {
	out := make([]cameraprofile.ProfileTemplateChannelInput, 0, len(channels))
	for _, channel := range channels {
		out = append(out, cameraprofile.ProfileTemplateChannelInput{
			Index:   channel.Index,
			Streams: profileTemplateMatcherStreams(channel.Streams),
		})
	}
	return out
}

func profileTemplateMatcherStreams(streams []store.CameraProfileTemplateStream) []cameraprofile.ProfileTemplateStreamInput {
	out := make([]cameraprofile.ProfileTemplateStreamInput, 0, len(streams))
	for _, stream := range streams {
		out = append(out, cameraprofile.ProfileTemplateStreamInput{
			Role:         cameraprofile.StreamRole(stream.Role),
			Source:       stream.Source,
			ProfileToken: stream.ProfileToken,
			Codec:        stream.Codec,
			Width:        stream.Width,
			Height:       stream.Height,
		})
	}
	return out
}

func redactDeviceScanResult(scan cameraprofile.DeviceScanResult) cameraprofile.DeviceScanResult {
	for channelIndex := range scan.Channels {
		for candidateIndex := range scan.Channels[channelIndex].Candidates {
			candidate := &scan.Channels[channelIndex].Candidates[candidateIndex]
			if candidate.RedactedURL == "" {
				candidate.RedactedURL = store.RedactURL(candidate.URL)
			}
			candidate.URL = ""
		}
	}
	return scan
}
