package cameraprofile

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestScannerDetectsVStarcamWhenONVIFIdentityIsGeneric(t *testing.T) {
	t.Parallel()

	client := fakeScannerClient{
		deviceInformation: `<tds:GetDeviceInformationResponse>
			<tds:Manufacturer>IP camera</tds:Manufacturer>
			<tds:Model>IP Camera</tds:Model>
			<tds:FirmwareVersion>2.4</tds:FirmwareVersion>
			<tds:SerialNumber>VSTAR-DUMMY-0001</tds:SerialNumber>
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

func TestScannerAddsReolinkClearHTTPFLVCandidate(t *testing.T) {
	t.Parallel()

	client := fakeScannerClient{
		deviceInformation: `<tds:GetDeviceInformationResponse>
			<tds:Manufacturer>Reolink</tds:Manufacturer>
			<tds:Model>Reolink Duo WiFi</tds:Model>
			<tds:FirmwareVersion>v3.0</tds:FirmwareVersion>
			<tds:SerialNumber>00000000000000</tds:SerialNumber>
			<tds:HardwareId>IPC</tds:HardwareId>
		</tds:GetDeviceInformationResponse>`,
		hostname: "reolink-duo",
		profiles: `<trt:Profiles token="mainStream">
			<tt:Name>mainStream</tt:Name>
			<tt:VideoEncoderConfiguration token="V_MAIN">
				<tt:Encoding>H264</tt:Encoding>
				<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>
				<tt:RateControl><tt:FrameRateLimit>22</tt:FrameRateLimit><tt:BitrateLimit>2048</tt:BitrateLimit></tt:RateControl>
			</tt:VideoEncoderConfiguration>
		</trt:Profiles>
		<trt:Profiles token="subStream">
			<tt:Name>subStream</tt:Name>
			<tt:VideoEncoderConfiguration token="V_SUB">
				<tt:Encoding>H264</tt:Encoding>
				<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>
				<tt:RateControl><tt:FrameRateLimit>10</tt:FrameRateLimit><tt:BitrateLimit>256</tt:BitrateLimit></tt:RateControl>
			</tt:VideoEncoderConfiguration>
		</trt:Profiles>`,
		streamURIs: map[string]string{
			"mainStream": "rtsp://192.168.0.12:554/h264Preview_01_main",
			"subStream":  "rtsp://192.168.0.12:554/h264Preview_01_sub",
		},
	}

	profile, err := NewScanner(client).Scan(t.Context(), ScanRequest{
		Host:      "192.168.0.12",
		Username:  "admin",
		Password:  "camera-pass",
		RTSPPort:  554,
		HTTPPort:  80,
		ONVIFPort: 8000,
		Adapter:   "auto",
	})
	if err != nil {
		t.Fatalf("scan reolink: %v", err)
	}

	if profile.Adapter != "reolink" {
		t.Fatalf("adapter = %q, want reolink", profile.Adapter)
	}
	candidates := profile.Channels[0].Candidates
	if len(candidates) != 3 {
		t.Fatalf("candidates = %d, want 3", len(candidates))
	}
	clear := candidates[0]
	if clear.RoleHint != StreamRoleRecording || clear.Source != "reolink-http-flv" {
		t.Fatalf("clear candidate = %#v", clear)
	}
	if clear.ProfileToken != "reolink-clear-main" || clear.Width != 1920 || clear.Height != 1080 || clear.BitrateKbps != 2048 {
		t.Fatalf("clear candidate metadata = %#v", clear)
	}
	parsed, err := url.Parse(clear.URL)
	if err != nil {
		t.Fatalf("parse clear URL: %v", err)
	}
	query := parsed.Query()
	if parsed.Scheme != "http" || parsed.Host != "192.168.0.12" || parsed.Path != "/flv" {
		t.Fatalf("clear URL endpoint = %s", clear.URL)
	}
	if query.Get("port") != "1935" || query.Get("app") != "bcs" || query.Get("stream") != "channel0_main.bcs" {
		t.Fatalf("clear URL query = %s", parsed.RawQuery)
	}
	if query.Get("user") != "admin" || query.Get("password") != "camera-pass" {
		t.Fatalf("clear URL credentials were not embedded for go2rtc")
	}
	if strings.Contains(clear.RedactedURL, "camera-pass") || strings.Contains(clear.RedactedURL, "admin") {
		t.Fatalf("redacted clear URL leaked credentials: %s", clear.RedactedURL)
	}
	if candidates[1].Source != "onvif" || candidates[2].RoleHint != StreamRoleLive {
		t.Fatalf("original ONVIF candidates should remain available: %#v", candidates)
	}
}

func TestScannerUsesReolinkSecondLensForClearHTTPFLVCandidate(t *testing.T) {
	t.Parallel()

	client := fakeScannerClient{
		deviceInformation: `<tds:GetDeviceInformationResponse>
			<tds:Manufacturer>Reolink</tds:Manufacturer>
			<tds:Model>Reolink Duo WiFi</tds:Model>
		</tds:GetDeviceInformationResponse>`,
		hostname: "reolink-duo",
		profiles: `<trt:Profiles token="mainStream">
			<tt:Name>mainStream</tt:Name>
			<tt:VideoEncoderConfiguration token="V_MAIN">
				<tt:Encoding>H264</tt:Encoding>
				<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>
				<tt:RateControl><tt:FrameRateLimit>22</tt:FrameRateLimit><tt:BitrateLimit>2048</tt:BitrateLimit></tt:RateControl>
			</tt:VideoEncoderConfiguration>
		</trt:Profiles>`,
		streamURIs: map[string]string{
			"mainStream": "rtsp://192.168.0.12:554/h264Preview_02_main",
		},
	}

	profile, err := NewScanner(client).Scan(t.Context(), ScanRequest{
		URL:       "rtsp://admin:camera-pass@192.168.0.12:554/h264Preview_02_main",
		HTTPPort:  80,
		ONVIFPort: 8000,
		Adapter:   "reolink",
	})
	if err != nil {
		t.Fatalf("scan reolink second lens: %v", err)
	}

	clear := profile.Channels[0].Candidates[0]
	parsed, err := url.Parse(clear.URL)
	if err != nil {
		t.Fatalf("parse clear URL: %v", err)
	}
	if got := parsed.Query().Get("stream"); got != "channel1_main.bcs" {
		t.Fatalf("clear FLV stream = %q, want channel1_main.bcs", got)
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
