package store

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type cameraProfileTemplatePayload struct {
	matchRules   string
	channels     string
	capabilities string
}

func normalizeCameraProfileTemplate(template CameraProfileTemplate) (CameraProfileTemplate, error) {
	template.ProfileName = strings.TrimSpace(template.ProfileName)
	template.Manufacturer = strings.TrimSpace(template.Manufacturer)
	template.Model = strings.TrimSpace(template.Model)
	template.Adapter = strings.TrimSpace(template.Adapter)
	if template.Version == 0 {
		template.Version = 1
	}
	if template.ProfileName == "" || template.Manufacturer == "" || template.Model == "" || template.Adapter == "" || template.Version < 1 {
		return CameraProfileTemplate{}, ErrProfileTemplateInvalid
	}
	if err := validateCredentialFreeProfileTemplate(template); err != nil {
		return CameraProfileTemplate{}, err
	}
	return template, nil
}

func marshalCameraProfileTemplatePayload(template CameraProfileTemplate) (cameraProfileTemplatePayload, error) {
	matchRules, err := json.Marshal(template.MatchRules)
	if err != nil {
		return cameraProfileTemplatePayload{}, fmt.Errorf("encode camera profile template match rules: %w", err)
	}
	channels, err := json.Marshal(template.Channels)
	if err != nil {
		return cameraProfileTemplatePayload{}, fmt.Errorf("encode camera profile template channels: %w", err)
	}
	capabilities, err := json.Marshal(template.Capabilities)
	if err != nil {
		return cameraProfileTemplatePayload{}, fmt.Errorf("encode camera profile template capabilities: %w", err)
	}
	return cameraProfileTemplatePayload{
		matchRules:   string(matchRules),
		channels:     string(channels),
		capabilities: string(capabilities),
	}, nil
}

func scanCameraProfileTemplate(row scanner) (CameraProfileTemplate, error) {
	var template CameraProfileTemplate
	var matchRulesJSON, channelsJSON, capabilitiesJSON, createdAt, updatedAt string
	if err := row.Scan(
		&template.ID,
		&template.ProfileName,
		&template.Manufacturer,
		&template.Model,
		&template.Adapter,
		&template.Version,
		&matchRulesJSON,
		&channelsJSON,
		&capabilitiesJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return CameraProfileTemplate{}, err
	}
	if err := json.Unmarshal([]byte(matchRulesJSON), &template.MatchRules); err != nil {
		return CameraProfileTemplate{}, fmt.Errorf("decode camera profile template match rules: %w", err)
	}
	if err := json.Unmarshal([]byte(channelsJSON), &template.Channels); err != nil {
		return CameraProfileTemplate{}, fmt.Errorf("decode camera profile template channels: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &template.Capabilities); err != nil {
		return CameraProfileTemplate{}, fmt.Errorf("decode camera profile template capabilities: %w", err)
	}
	template.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	template.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return template, nil
}

func normalizeProfileTemplateKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func validateCredentialFreeProfileTemplate(template CameraProfileTemplate) error {
	for _, channel := range template.Channels {
		for _, stream := range channel.Streams {
			if strings.Contains(stream.Path, "://") || strings.Contains(stream.Path, "@") || containsCredentialQuery(stream.Path) {
				return fmt.Errorf("stream path %q: %w", stream.Path, ErrProfileTemplateInvalid)
			}
		}
	}
	return nil
}

func containsCredentialQuery(path string) bool {
	parsed, err := url.Parse(path)
	if err != nil {
		return true
	}
	for key := range parsed.Query() {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "user", "username", "password", "passwd", "pwd", "token":
			return true
		}
	}
	return false
}

func profileTemplateWriteError(action string, err error) error {
	if strings.Contains(err.Error(), "idx_camera_profile_templates_unique_key") ||
		strings.Contains(err.Error(), "camera_profile_templates.normalized_adapter") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("%s: %w", action, ErrProfileTemplateDuplicate)
	}
	return fmt.Errorf("%s: %w", action, err)
}
