package main

import (
	"fmt"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

func scanReqHasTarget(req cameraprofile.ScanRequest) bool {
	return req.Host != "" || req.URL != ""
}

func hasProfileCandidates(profile cameraprofile.DeviceProfile) bool {
	return len(profileCandidates(profile)) > 0
}

func hasProfileCandidateURLs(profile cameraprofile.DeviceProfile) bool {
	for _, candidate := range profileCandidates(profile) {
		if candidate.URL != "" {
			return true
		}
	}
	return false
}

func profileCandidates(profile cameraprofile.DeviceProfile) []cameraprofile.StreamCandidate {
	var candidates []cameraprofile.StreamCandidate
	for _, channel := range profile.Channels {
		candidates = append(candidates, channel.Candidates...)
	}
	return candidates
}

func primaryCandidateURL(candidates []cameraprofile.StreamCandidate) string {
	for _, candidate := range candidates {
		if candidate.RoleHint == cameraprofile.StreamRoleRecording && candidate.URL != "" {
			return candidate.URL
		}
	}
	for _, candidate := range candidates {
		if candidate.URL != "" {
			return candidate.URL
		}
	}
	return ""
}

func selectProfileCandidates(profile cameraprofile.DeviceProfile, channelIndex int, selections []cameraStreamSelection) []cameraprofile.StreamCandidate {
	channel := profileChannel(profile, channelIndex)
	if channel == nil {
		return nil
	}
	selected := make([]cameraprofile.StreamCandidate, 0, len(selections))
	for _, selection := range selections {
		if selection.ProfileToken == "" {
			continue
		}
		candidate, ok := candidateByProfileToken(channel.Candidates, selection.ProfileToken)
		if !ok {
			continue
		}
		if selection.Role != "" {
			candidate.RoleHint = selection.Role
		}
		selected = append(selected, candidate)
	}
	return selected
}

func profileChannel(profile cameraprofile.DeviceProfile, channelIndex int) *cameraprofile.ChannelProfile {
	for i := range profile.Channels {
		if profile.Channels[i].Index == channelIndex {
			return &profile.Channels[i]
		}
	}
	if len(profile.Channels) == 0 {
		return nil
	}
	return &profile.Channels[0]
}

func candidateByProfileToken(candidates []cameraprofile.StreamCandidate, token string) (cameraprofile.StreamCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.ProfileToken == token {
			return candidate, true
		}
	}
	return cameraprofile.StreamCandidate{}, false
}

func toStoreStreams(base string, candidates []cameraprofile.StreamCandidate, state string) []store.CameraStream {
	streams := make([]store.CameraStream, 0, 2)
	appendCandidate := func(candidate cameraprofile.StreamCandidate, role store.CameraStreamRole) {
		streams = append(streams, store.CameraStream{
			Role:             role,
			SourceKey:        string(role),
			Label:            candidate.Label,
			Source:           candidate.Source,
			URL:              candidate.URL,
			Go2RTCStreamName: roleStreamName(base, role),
			Codec:            candidate.Codec,
			Width:            candidate.Width,
			Height:           candidate.Height,
			FPS:              candidate.FPS,
			BitrateKbps:      candidate.BitrateKbps,
			ProfileToken:     candidate.ProfileToken,
			State:            state,
		})
	}
	var recording, live *cameraprofile.StreamCandidate
	for i := range candidates {
		if candidates[i].URL == "" {
			continue
		}
		switch candidates[i].RoleHint {
		case cameraprofile.StreamRoleRecording:
			if recording == nil {
				recording = &candidates[i]
			}
		case cameraprofile.StreamRoleLive:
			if live == nil {
				live = &candidates[i]
			}
		}
	}
	if recording == nil {
		for i := range candidates {
			if candidates[i].URL != "" {
				recording = &candidates[i]
				break
			}
		}
	}
	if recording != nil {
		appendCandidate(*recording, store.CameraStreamRoleRecording)
	}
	if live != nil && (recording == nil || live.URL != recording.URL) {
		appendCandidate(*live, store.CameraStreamRoleLive)
	}
	return streams
}

func roleStreamName(base string, role store.CameraStreamRole) string {
	switch role {
	case store.CameraStreamRoleLive:
		return base + "-live"
	case store.CameraStreamRoleSnapshot:
		return base + "-snapshot"
	default:
		return base + "-recording"
	}
}

func redactDeviceProfile(profile cameraprofile.DeviceProfile) cameraprofile.DeviceProfile {
	producerKeys := map[string]string{}
	for channelIndex := range profile.Channels {
		for candidateIndex := range profile.Channels[channelIndex].Candidates {
			candidate := &profile.Channels[channelIndex].Candidates[candidateIndex]
			redactStreamCandidate(candidate, producerKeys)
		}
	}
	return profile
}

func redactStreamCandidate(candidate *cameraprofile.StreamCandidate, producerKeys map[string]string) {
	rawURL := candidate.URL
	candidate.ProducerKey = ""
	if rawURL != "" {
		key, ok := producerKeys[rawURL]
		if !ok {
			key = fmt.Sprintf("producer-%d", len(producerKeys)+1)
			producerKeys[rawURL] = key
		}
		candidate.ProducerKey = key
	}
	if candidate.RedactedURL == "" {
		candidate.RedactedURL = store.RedactURL(rawURL)
	}
	candidate.URL = ""
}

func sanitizeCameraSecrets(camera *store.Camera) {
	camera.URL = ""
	for i := range camera.Streams {
		if camera.Streams[i].RedactedURL == "" {
			camera.Streams[i].RedactedURL = store.RedactURL(camera.Streams[i].URL)
		}
		camera.Streams[i].URL = ""
	}
}
