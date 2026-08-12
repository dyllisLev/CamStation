package cameraprofile

type scanStreamSignature struct {
	Role         StreamRole
	Source       string
	ProfileToken string
	Codec        string
	Width        int
	Height       int
}

func matchingScanResult(adapter string, manufacturer string, model string, signature scanStreamSignature) DeviceScanResult {
	return DeviceScanResult{
		Host:         "192.168.0.10",
		Manufacturer: manufacturer,
		Model:        model,
		Adapter:      adapter,
		Channels: []ScanChannel{{
			Index: 0,
			Label: "channel 0",
			Candidates: []StreamCandidate{{
				RoleHint:     signature.Role,
				Label:        "main",
				Source:       signature.Source,
				Codec:        signature.Codec,
				Width:        signature.Width,
				Height:       signature.Height,
				ProfileToken: signature.ProfileToken,
			}},
		}},
	}
}
