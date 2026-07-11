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
	streams := make([]store.CameraStream, 0, len(candidates))
	used := map[string]int{}
	for _, candidate := range candidates {
		if candidate.URL == "" {
			continue
		}
		role := store.CameraStreamRole(candidate.RoleHint)
		if role == "" {
			role = store.CameraStreamRoleRecording
		}
		name := roleStreamName(base, role)
		if used[name] > 0 {
			used[name]++
			name = fmt.Sprintf("%s-%d", name, used[name])
		} else {
			used[name] = 1
		}
		streams = append(streams, store.CameraStream{
			Role:             role,
			SourceKey:        string(role),
			Label:            candidate.Label,
			Source:           candidate.Source,
			URL:              candidate.URL,
			Go2RTCStreamName: name,
			Codec:            candidate.Codec,
			Width:            candidate.Width,
			Height:           candidate.Height,
			FPS:              candidate.FPS,
			BitrateKbps:      candidate.BitrateKbps,
			ProfileToken:     candidate.ProfileToken,
			State:            state,
		})
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
	for channelIndex := range profile.Channels {
		for candidateIndex := range profile.Channels[channelIndex].Candidates {
			candidate := &profile.Channels[channelIndex].Candidates[candidateIndex]
			if candidate.RedactedURL == "" {
				candidate.RedactedURL = store.RedactURL(candidate.URL)
			}
			candidate.URL = ""
		}
	}
	return profile
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
