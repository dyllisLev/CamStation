package cameraprofile

import (
	"sort"
	"strings"
)

const minimumTemplateMatchConfidence = 35

type ProfileTemplateInput struct {
	ID           string
	Name         string
	Adapter      string
	Manufacturer string
	Model        string
	Channels     []ProfileTemplateChannelInput
}

type ProfileTemplateChannelInput struct {
	Index   int
	Streams []ProfileTemplateStreamInput
}

type ProfileTemplateStreamInput struct {
	Role         StreamRole
	Source       string
	ProfileToken string
	Codec        string
	Width        int
	Height       int
}

type profileTemplateInput = ProfileTemplateInput
type profileTemplateChannelInput = ProfileTemplateChannelInput
type profileTemplateStreamInput = ProfileTemplateStreamInput

type ProfileTemplateMatchResult struct {
	Matches        []ProfileTemplateMatch `json:"matches"`
	Recommendation *ProfileTemplateMatch  `json:"recommendation,omitempty"`
}

type ProfileTemplateMatch struct {
	TemplateID string   `json:"templateId"`
	Name       string   `json:"name"`
	Confidence int      `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

func MatchProfileTemplates(scan DeviceScanResult, templates []profileTemplateInput) ProfileTemplateMatchResult {
	matches := make([]ProfileTemplateMatch, 0, len(templates))
	for _, template := range templates {
		match := scoreProfileTemplate(scan, template)
		if match.Confidence < minimumTemplateMatchConfidence {
			continue
		}
		matches = append(matches, match)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Confidence == matches[j].Confidence {
			return matches[i].TemplateID < matches[j].TemplateID
		}
		return matches[i].Confidence > matches[j].Confidence
	})

	var recommendation *ProfileTemplateMatch
	if len(matches) == 1 || len(matches) > 1 && matches[0].Confidence > matches[1].Confidence {
		selected := matches[0]
		recommendation = &selected
	}
	return ProfileTemplateMatchResult{Matches: matches, Recommendation: recommendation}
}

func scoreProfileTemplate(scan DeviceScanResult, template profileTemplateInput) ProfileTemplateMatch {
	confidence := 0
	reasons := make([]string, 0, 4)
	if sameFold(scan.Adapter, template.Adapter) {
		confidence += 35
		reasons = append(reasons, "adapter")
	}
	if sameFold(scan.Manufacturer, template.Manufacturer) {
		confidence += 20
		reasons = append(reasons, "manufacturer")
	} else if isGenericCameraIdentity(scan.Manufacturer, scan.Model) && template.Manufacturer != "" {
		confidence += 5
		reasons = append(reasons, "generic-manufacturer")
	}
	if sameFold(scan.Model, template.Model) {
		confidence += 20
		reasons = append(reasons, "model")
	}
	streamScore := scoreTemplateStreams(scan, template)
	if streamScore > 0 {
		confidence += streamScore
		reasons = append(reasons, "stream-signature")
	}
	if confidence > 100 {
		confidence = 100
	}
	return ProfileTemplateMatch{
		TemplateID: template.ID,
		Name:       template.Name,
		Confidence: confidence,
		Reasons:    reasons,
	}
}

func scoreTemplateStreams(scan DeviceScanResult, template profileTemplateInput) int {
	score := 0
	for _, channel := range template.Channels {
		for _, stream := range channel.Streams {
			if scanHasStreamSignature(scan, channel.Index, stream) {
				score += 25
			}
		}
	}
	if score > 25 {
		return 25
	}
	return score
}

func scanHasStreamSignature(scan DeviceScanResult, channelIndex int, stream profileTemplateStreamInput) bool {
	for _, channel := range scan.Channels {
		if channel.Index != channelIndex {
			continue
		}
		for _, candidate := range channel.Candidates {
			if streamMatchesCandidate(stream, candidate) {
				return true
			}
		}
	}
	return false
}

func streamMatchesCandidate(stream profileTemplateStreamInput, candidate StreamCandidate) bool {
	if stream.Role != "" && stream.Role != candidate.RoleHint {
		return false
	}
	if stream.Source != "" && !sameFold(stream.Source, candidate.Source) {
		return false
	}
	if stream.ProfileToken != "" && !sameFold(stream.ProfileToken, candidate.ProfileToken) {
		return false
	}
	if stream.Codec != "" && !sameFold(stream.Codec, candidate.Codec) {
		return false
	}
	if stream.Width != 0 && stream.Width != candidate.Width {
		return false
	}
	if stream.Height != 0 && stream.Height != candidate.Height {
		return false
	}
	return true
}

func sameFold(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func isGenericCameraIdentity(manufacturer string, model string) bool {
	manufacturer = strings.TrimSpace(strings.ToLower(manufacturer))
	model = strings.TrimSpace(strings.ToLower(model))
	return manufacturer == "ip camera" || manufacturer == "ipcamera" || model == "ip camera" || model == "ipcamera"
}
