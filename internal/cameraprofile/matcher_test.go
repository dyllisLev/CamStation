package cameraprofile

import "testing"

func TestProfileTemplateMatcher_returnsExactMatchRecommendation_whenAdapterManufacturerModelAndStreamsMatch(t *testing.T) {
	t.Parallel()

	// Given
	scan := matchingScanResult("reolink", "Reolink", "Duo 2", scanStreamSignature{
		Role:         StreamRoleRecording,
		Source:       "onvif",
		ProfileToken: "mainStream",
		Codec:        "h264",
		Width:        2560,
		Height:       1440,
	})
	templates := []profileTemplateInput{{
		ID:           "tpl-reolink-duo2",
		Name:         "Reolink Duo 2",
		Adapter:      "reolink",
		Manufacturer: "Reolink",
		Model:        "Duo 2",
		Channels: []profileTemplateChannelInput{{
			Index: 0,
			Streams: []profileTemplateStreamInput{{
				Role:         StreamRoleRecording,
				Source:       "onvif",
				ProfileToken: "mainStream",
				Codec:        "h264",
				Width:        2560,
				Height:       1440,
			}},
		}},
	}}

	// When
	result := MatchProfileTemplates(scan, templates)

	// Then
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(result.Matches))
	}
	if result.Recommendation == nil {
		t.Fatalf("recommendation is nil, want exact match")
	}
	if result.Recommendation.TemplateID != "tpl-reolink-duo2" {
		t.Fatalf("recommendation = %q, want tpl-reolink-duo2", result.Recommendation.TemplateID)
	}
	if result.Matches[0].Confidence < 90 {
		t.Fatalf("confidence = %d, want exact match >= 90", result.Matches[0].Confidence)
	}
}

func TestProfileTemplateMatcher_matchesGenericManufacturer_whenStreamSignatureMatches(t *testing.T) {
	t.Parallel()

	// Given
	scan := matchingScanResult("vstarcam", "IP camera", "IP Camera", scanStreamSignature{
		Role:         StreamRoleRecording,
		Source:       "onvif-vstarcam",
		ProfileToken: "PROFILE_000",
		Codec:        "h264",
		Width:        2304,
		Height:       1296,
	})
	templates := []profileTemplateInput{{
		ID:           "tpl-vstarcam-generic",
		Name:         "VStarcam generic VeePai",
		Adapter:      "vstarcam",
		Manufacturer: "VStarcam",
		Model:        "VeePai IP Camera",
		Channels: []profileTemplateChannelInput{{
			Index: 0,
			Streams: []profileTemplateStreamInput{{
				Role:         StreamRoleRecording,
				Source:       "onvif-vstarcam",
				ProfileToken: "PROFILE_000",
				Codec:        "h264",
				Width:        2304,
				Height:       1296,
			}},
		}},
	}}

	// When
	result := MatchProfileTemplates(scan, templates)

	// Then
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(result.Matches))
	}
	if result.Matches[0].TemplateID != "tpl-vstarcam-generic" {
		t.Fatalf("match = %q, want tpl-vstarcam-generic", result.Matches[0].TemplateID)
	}
	if result.Recommendation == nil {
		t.Fatalf("recommendation is nil, want stream-signature match")
	}
}

func TestProfileTemplateMatcher_returnsNoMatches_whenIdentityAndStreamsDoNotMatch(t *testing.T) {
	t.Parallel()

	// Given
	scan := matchingScanResult("onvif", "Unknown", "Unknown", scanStreamSignature{
		Role:         StreamRoleRecording,
		Source:       "onvif",
		ProfileToken: "profile-a",
		Codec:        "h265",
		Width:        1280,
		Height:       720,
	})
	templates := []profileTemplateInput{{
		ID:           "tpl-reolink-duo2",
		Name:         "Reolink Duo 2",
		Adapter:      "reolink",
		Manufacturer: "Reolink",
		Model:        "Duo 2",
		Channels: []profileTemplateChannelInput{{
			Index: 0,
			Streams: []profileTemplateStreamInput{{
				Role:         StreamRoleRecording,
				Source:       "reolink-http-flv",
				ProfileToken: "reolink-clear-main",
				Codec:        "h264",
				Width:        2560,
				Height:       1440,
			}},
		}},
	}}

	// When
	result := MatchProfileTemplates(scan, templates)

	// Then
	if len(result.Matches) != 0 {
		t.Fatalf("matches = %d, want 0", len(result.Matches))
	}
	if result.Recommendation != nil {
		t.Fatalf("recommendation = %#v, want nil", result.Recommendation)
	}
}

func TestProfileTemplateMatcher_returnsMultipleMatchesSortedByConfidence(t *testing.T) {
	t.Parallel()

	// Given
	scan := matchingScanResult("reolink", "Reolink", "Duo 2", scanStreamSignature{
		Role:         StreamRoleRecording,
		Source:       "onvif",
		ProfileToken: "mainStream",
		Codec:        "h264",
		Width:        2560,
		Height:       1440,
	})
	templates := []profileTemplateInput{
		{
			ID:           "tpl-adapter-only",
			Name:         "Adapter only",
			Adapter:      "reolink",
			Manufacturer: "Reolink",
			Model:        "Other",
		},
		{
			ID:           "tpl-exact",
			Name:         "Exact",
			Adapter:      "reolink",
			Manufacturer: "Reolink",
			Model:        "Duo 2",
			Channels: []profileTemplateChannelInput{{
				Index: 0,
				Streams: []profileTemplateStreamInput{{
					Role:         StreamRoleRecording,
					Source:       "onvif",
					ProfileToken: "mainStream",
					Codec:        "h264",
					Width:        2560,
					Height:       1440,
				}},
			}},
		},
		{
			ID:           "tpl-stream",
			Name:         "Stream",
			Adapter:      "reolink",
			Manufacturer: "Reolink",
			Model:        "Duo",
			Channels: []profileTemplateChannelInput{{
				Index: 0,
				Streams: []profileTemplateStreamInput{{
					Role:         StreamRoleRecording,
					Source:       "onvif",
					ProfileToken: "mainStream",
					Codec:        "h264",
					Width:        2560,
					Height:       1440,
				}},
			}},
		},
	}

	// When
	result := MatchProfileTemplates(scan, templates)

	// Then
	if len(result.Matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(result.Matches))
	}
	got := []string{result.Matches[0].TemplateID, result.Matches[1].TemplateID, result.Matches[2].TemplateID}
	want := []string{"tpl-exact", "tpl-stream", "tpl-adapter-only"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("match order = %v, want %v", got, want)
		}
	}
}
