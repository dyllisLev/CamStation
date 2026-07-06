package cameraprofile

import "testing"

func TestProfileTemplateMatcher_returnsNoRecommendation_whenTopMatchesAreAmbiguous(t *testing.T) {
	t.Parallel()

	// Given
	scan := matchingScanResult("onvif", "Acme", "A1", scanStreamSignature{
		Role:         StreamRoleRecording,
		Source:       "onvif",
		ProfileToken: "main",
		Codec:        "h264",
		Width:        1920,
		Height:       1080,
	})
	templates := []profileTemplateInput{
		{
			ID:           "tpl-a",
			Name:         "Template A",
			Adapter:      "onvif",
			Manufacturer: "Acme",
			Model:        "A1",
		},
		{
			ID:           "tpl-b",
			Name:         "Template B",
			Adapter:      "onvif",
			Manufacturer: "Acme",
			Model:        "A1",
		},
	}

	// When
	result := MatchProfileTemplates(scan, templates)

	// Then
	if len(result.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(result.Matches))
	}
	if result.Recommendation != nil {
		t.Fatalf("recommendation = %#v, want nil for ambiguous top matches", result.Recommendation)
	}
}

func TestProfileTemplateMatcher_returnsRecommendation_whenTopMatchIsUnambiguous(t *testing.T) {
	t.Parallel()

	// Given
	scan := matchingScanResult("onvif", "Acme", "A1", scanStreamSignature{
		Role:         StreamRoleRecording,
		Source:       "onvif",
		ProfileToken: "main",
		Codec:        "h264",
		Width:        1920,
		Height:       1080,
	})
	templates := []profileTemplateInput{
		{
			ID:           "tpl-generic",
			Name:         "Generic Acme",
			Adapter:      "onvif",
			Manufacturer: "Acme",
			Model:        "A-series",
		},
		{
			ID:           "tpl-a1",
			Name:         "Acme A1",
			Adapter:      "onvif",
			Manufacturer: "Acme",
			Model:        "A1",
			Channels: []profileTemplateChannelInput{{
				Index: 0,
				Streams: []profileTemplateStreamInput{{
					Role:         StreamRoleRecording,
					Source:       "onvif",
					ProfileToken: "main",
					Codec:        "h264",
					Width:        1920,
					Height:       1080,
				}},
			}},
		},
	}

	// When
	result := MatchProfileTemplates(scan, templates)

	// Then
	if result.Recommendation == nil {
		t.Fatalf("recommendation is nil, want tpl-a1")
	}
	if result.Recommendation.TemplateID != "tpl-a1" {
		t.Fatalf("recommendation = %q, want tpl-a1", result.Recommendation.TemplateID)
	}
}
