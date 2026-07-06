package main

import (
	"context"

	"camstation/internal/cameraprofile"
)

func validateCameraMutationTargets(ctx context.Context, req cameraCreateRequest) error {
	scanReq := scanRequestFromCamera(req)
	if scanReqHasTarget(scanReq) {
		if err := validateScanTarget(ctx, scanReq); err != nil {
			return err
		}
	}
	for _, rawURL := range requestSuppliedCandidateURLs(req) {
		if err := validateProbeTarget(ctx, rawURL); err != nil {
			return err
		}
	}
	return nil
}

func requestSuppliedCandidateURLs(req cameraCreateRequest) []string {
	urls := make([]string, 0, 1+len(req.Streams)+len(profileCandidates(req.Profile)))
	if req.URL != "" {
		urls = append(urls, req.URL)
	}
	appendCandidateURL := func(candidate cameraprofile.StreamCandidate) {
		if candidate.URL != "" {
			urls = append(urls, candidate.URL)
		}
	}
	for _, candidate := range req.Streams {
		appendCandidateURL(candidate)
	}
	for _, candidate := range profileCandidates(req.Profile) {
		appendCandidateURL(candidate)
	}
	return urls
}
