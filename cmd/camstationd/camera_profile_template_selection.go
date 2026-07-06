package main

import (
	"fmt"
	"net/url"
	"path"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

func mergeTemplateIntoDeviceProfile(profile cameraprofile.DeviceProfile, template store.CameraProfileTemplate, req cameraCreateRequest) cameraprofile.DeviceProfile {
	if profile.Manufacturer == "" {
		profile.Manufacturer = template.Manufacturer
	}
	if profile.Model == "" {
		profile.Model = template.Model
	}
	if profile.Adapter == "" {
		profile.Adapter = template.Adapter
	}
	if profile.Host == "" {
		profile.Host = req.Host
	}
	if profile.RTSPPort == 0 {
		profile.RTSPPort = req.RTSPPort
	}
	if profile.HTTPPort == 0 {
		profile.HTTPPort = req.HTTPPort
	}
	if profile.ONVIFPort == 0 {
		profile.ONVIFPort = req.ONVIFPort
	}
	return profileWithCandidates(profile, profileTemplateCandidates(template, req))
}

func profileWithCandidates(profile cameraprofile.DeviceProfile, candidates []cameraprofile.StreamCandidate) cameraprofile.DeviceProfile {
	if len(profile.Channels) > 0 {
		return profile
	}
	if len(candidates) == 0 {
		return profile
	}
	profile.Channels = []cameraprofile.ChannelProfile{{
		Index:      0,
		Label:      "channel 0",
		Candidates: candidates,
	}}
	return profile
}

func profileTemplateCandidates(template store.CameraProfileTemplate, req cameraCreateRequest) []cameraprofile.StreamCandidate {
	candidates := make([]cameraprofile.StreamCandidate, 0)
	for _, channel := range template.Channels {
		for _, stream := range channel.Streams {
			rawURL := profileTemplateStreamURL(req, stream)
			if rawURL == "" {
				continue
			}
			candidates = append(candidates, cameraprofile.StreamCandidate{
				RoleHint:     cameraprofile.StreamRole(stream.Role),
				Label:        stream.Label,
				Source:       stream.Source,
				URL:          rawURL,
				RedactedURL:  store.RedactURL(rawURL),
				Codec:        stream.Codec,
				Width:        stream.Width,
				Height:       stream.Height,
				FPS:          stream.FPS,
				BitrateKbps:  stream.BitrateKbps,
				ProfileToken: stream.ProfileToken,
			})
		}
	}
	return candidates
}

func profileTemplateStreamURL(req cameraCreateRequest, stream store.CameraProfileTemplateStream) string {
	if stream.Path == "" || req.Host == "" {
		return ""
	}
	port := req.RTSPPort
	if port == 0 {
		port = 554
	}
	u := url.URL{
		Scheme: "rtsp",
		Host:   fmt.Sprintf("%s:%d", req.Host, port),
		Path:   path.Clean("/" + stream.Path),
	}
	if req.Username != "" {
		if req.Password != "" {
			u.User = url.UserPassword(req.Username, req.Password)
		} else {
			u.User = url.User(req.Username)
		}
	}
	return u.String()
}
