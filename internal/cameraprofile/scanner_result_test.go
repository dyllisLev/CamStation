package cameraprofile

import (
	"context"
	"testing"
)

func TestScannerScanResult_returnsMultiChannelCandidateShape_whenStreamURIsContainChannelIndexes(t *testing.T) {
	t.Parallel()

	// Given
	client := fakeScannerClient{
		deviceInformation: `<tds:GetDeviceInformationResponse>
			<tds:Manufacturer>VStarcam</tds:Manufacturer>
			<tds:Model>Dual Lens</tds:Model>
		</tds:GetDeviceInformationResponse>`,
		hostname: "dual-lens",
		profiles: `<trt:Profiles token="PROFILE_000">
				<tt:Name>PROFILE_000</tt:Name>
				<tt:VideoEncoderConfiguration token="V_ENC_000">
					<tt:Encoding>H264</tt:Encoding>
					<tt:Resolution><tt:Width>2304</tt:Width><tt:Height>1296</tt:Height></tt:Resolution>
					<tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>1024</tt:BitrateLimit></tt:RateControl>
				</tt:VideoEncoderConfiguration>
			</trt:Profiles>
			<trt:Profiles token="PROFILE_001">
				<tt:Name>PROFILE_001</tt:Name>
				<tt:VideoEncoderConfiguration token="V_ENC_001">
					<tt:Encoding>H264</tt:Encoding>
					<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>
					<tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>512</tt:BitrateLimit></tt:RateControl>
				</tt:VideoEncoderConfiguration>
			</trt:Profiles>
			<trt:Profiles token="PROFILE_100">
				<tt:Name>PROFILE_100</tt:Name>
				<tt:VideoEncoderConfiguration token="V_ENC_100">
					<tt:Encoding>H264</tt:Encoding>
					<tt:Resolution><tt:Width>2304</tt:Width><tt:Height>1296</tt:Height></tt:Resolution>
					<tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>1024</tt:BitrateLimit></tt:RateControl>
				</tt:VideoEncoderConfiguration>
			</trt:Profiles>
			<trt:Profiles token="PROFILE_101">
				<tt:Name>PROFILE_101</tt:Name>
				<tt:VideoEncoderConfiguration token="V_ENC_101">
					<tt:Encoding>H264</tt:Encoding>
					<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>
					<tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>512</tt:BitrateLimit></tt:RateControl>
				</tt:VideoEncoderConfiguration>
			</trt:Profiles>`,
		streamURIs: map[string]string{
			"PROFILE_000": "rtsp://192.168.0.55:10554/tcp/av0_0",
			"PROFILE_001": "rtsp://192.168.0.55:10554/tcp/av0_1",
			"PROFILE_100": "rtsp://192.168.0.55:10554/tcp/av1_0",
			"PROFILE_101": "rtsp://192.168.0.55:10554/tcp/av1_1",
		},
	}

	// When
	result, err := NewScanner(client).ScanResult(context.Background(), ScanRequest{
		Host:      "192.168.0.55",
		RTSPPort:  10554,
		HTTPPort:  10080,
		ONVIFPort: 10080,
		Adapter:   "auto",
	})

	// Then
	if err != nil {
		t.Fatalf("scan result: %v", err)
	}
	if len(result.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(result.Channels))
	}
	if result.Channels[0].Index != 0 || len(result.Channels[0].Candidates) != 2 {
		t.Fatalf("channel 0 = %#v, want index 0 with 2 candidates", result.Channels[0])
	}
	if result.Channels[1].Index != 1 || len(result.Channels[1].Candidates) != 2 {
		t.Fatalf("channel 1 = %#v, want index 1 with 2 candidates", result.Channels[1])
	}
	if result.Channels[1].Candidates[0].RoleHint != StreamRoleRecording {
		t.Fatalf("channel 1 first role = %q, want recording", result.Channels[1].Candidates[0].RoleHint)
	}
	if result.Channels[1].Candidates[1].RoleHint != StreamRoleLive {
		t.Fatalf("channel 1 second role = %q, want live", result.Channels[1].Candidates[1].RoleHint)
	}
}
