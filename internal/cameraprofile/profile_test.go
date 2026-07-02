package cameraprofile

import (
	"context"
	"testing"
)

func TestScannerDetectsVStarcamWhenONVIFIdentityIsGeneric(t *testing.T) {
	t.Parallel()

	client := fakeScannerClient{
		deviceInformation: `<tds:GetDeviceInformationResponse>
			<tds:Manufacturer>IP camera</tds:Manufacturer>
			<tds:Model>IP Camera</tds:Model>
			<tds:FirmwareVersion>2.4</tds:FirmwareVersion>
			<tds:SerialNumber>AAC2362423PHIH</tds:SerialNumber>
			<tds:HardwareId>1.0</tds:HardwareId>
		</tds:GetDeviceInformationResponse>`,
		hostname: "veepai",
		profiles: `<trt:Profiles token="PROFILE_000">
			<tt:Name>PROFILE_000</tt:Name>
			<tt:VideoEncoderConfiguration token="V_ENC_000">
				<tt:Encoding>H264</tt:Encoding>
				<tt:Resolution><tt:Width>2304</tt:Width><tt:Height>1296</tt:Height></tt:Resolution>
				<tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>1024</tt:BitrateLimit></tt:RateControl>
			</tt:VideoEncoderConfiguration>
			<tt:AudioEncoderConfiguration token="A_ENC_000"><tt:Encoding>G711</tt:Encoding></tt:AudioEncoderConfiguration>
			<tt:PTZConfiguration token="PTZ_000" />
		</trt:Profiles>
		<trt:Profiles token="PROFILE_001">
			<tt:Name>PROFILE_001</tt:Name>
			<tt:VideoEncoderConfiguration token="V_ENC_001">
				<tt:Encoding>H264</tt:Encoding>
				<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>
				<tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>512</tt:BitrateLimit></tt:RateControl>
			</tt:VideoEncoderConfiguration>
			<tt:AudioEncoderConfiguration token="A_ENC_000"><tt:Encoding>G711</tt:Encoding></tt:AudioEncoderConfiguration>
			<tt:PTZConfiguration token="PTZ_000" />
		</trt:Profiles>`,
		streamURIs: map[string]string{
			"PROFILE_000": "rtsp://192.168.0.55:10554/tcp/av0_0",
			"PROFILE_001": "rtsp://192.168.0.55:10554/tcp/av0_1",
		},
		ptz: PTZSummary{Supported: true, MaxPresets: 100},
	}

	profile, err := NewScanner(client).Scan(t.Context(), ScanRequest{
		Name:      "염소장",
		Host:      "192.168.0.55",
		Username:  "admin",
		Password:  "secret",
		RTSPPort:  10554,
		HTTPPort:  10080,
		ONVIFPort: 10080,
		Adapter:   "auto",
	})
	if err != nil {
		t.Fatalf("scan vstarcam: %v", err)
	}

	if profile.Adapter != "vstarcam" {
		t.Fatalf("adapter = %q, want vstarcam", profile.Adapter)
	}
	if profile.Manufacturer != "VStarcam" || profile.Model != "VeePai IP Camera" {
		t.Fatalf("identity = %s/%s, want VStarcam/VeePai IP Camera", profile.Manufacturer, profile.Model)
	}
	if profile.LastScan["onvifDeviceInformation"] == nil {
		t.Fatalf("last scan should preserve generic ONVIF identity")
	}
	if len(profile.Channels) != 1 {
		t.Fatalf("channels = %d, want 1", len(profile.Channels))
	}
	candidates := profile.Channels[0].Candidates
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}
	if candidates[0].RoleHint != StreamRoleRecording || candidates[0].URL == "" {
		t.Fatalf("recording candidate = %#v", candidates[0])
	}
	if candidates[1].RoleHint != StreamRoleLive || candidates[1].URL == "" {
		t.Fatalf("live candidate = %#v", candidates[1])
	}
	if !profile.Capabilities.PTZ || profile.Capabilities.MaxPresets != 100 {
		t.Fatalf("capabilities = %#v", profile.Capabilities)
	}
}

type fakeScannerClient struct {
	deviceInformation string
	hostname          string
	profiles          string
	streamURIs        map[string]string
	ptz               PTZSummary
}

func (f fakeScannerClient) DeviceInformation(_ context.Context, _ ScanRequest) (string, error) {
	return f.deviceInformation, nil
}

func (f fakeScannerClient) Hostname(_ context.Context, _ ScanRequest) (string, error) {
	return f.hostname, nil
}

func (f fakeScannerClient) Profiles(_ context.Context, _ ScanRequest) (string, error) {
	return f.profiles, nil
}

func (f fakeScannerClient) StreamURI(_ context.Context, _ ScanRequest, token string) (string, error) {
	return f.streamURIs[token], nil
}

func (f fakeScannerClient) PTZSummary(_ context.Context, _ ScanRequest, _ string) (PTZSummary, error) {
	return f.ptz, nil
}
