package cameraprofile

import "strconv"

func channelIndexForCandidate(candidate StreamCandidate) int {
	for _, value := range []string{candidate.URL, candidate.RedactedURL, candidate.ProfileToken, candidate.Label} {
		if index, ok := vstarcamChannelIndex(value); ok {
			return index
		}
		if index, ok := reolinkChannelIndex(value); ok {
			return index
		}
	}
	return 0
}

func vstarcamChannelIndex(value string) (int, bool) {
	channel, _, ok := vstarcamChannelAndStream(value)
	return channel, ok
}

func vstarcamChannelAndStream(value string) (int, int, bool) {
	matches := vstarcamStreamPathRE.FindStringSubmatch(value)
	if len(matches) != 3 {
		return 0, 0, false
	}
	channel, channelErr := strconv.Atoi(matches[1])
	stream, streamErr := strconv.Atoi(matches[2])
	if channelErr != nil || streamErr != nil {
		return 0, 0, false
	}
	return channel, stream, true
}

func reolinkChannelIndex(value string) (int, bool) {
	if matches := reolinkPreviewPathRE.FindStringSubmatch(value); len(matches) == 2 {
		number, err := strconv.Atoi(matches[1])
		if err == nil && number > 0 {
			return number - 1, true
		}
	}
	if matches := reolinkChannelRE.FindStringSubmatch(value); len(matches) == 2 {
		number, err := strconv.Atoi(matches[1])
		if err == nil && number >= 0 {
			return number, true
		}
	}
	return 0, false
}
