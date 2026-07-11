package camera

import (
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	got := RedactURL("rtsp://user:secret@10.0.0.10:554/live")
	want := "rtsp://redacted:redacted@10.0.0.10:554/live"
	if got != want {
		t.Fatalf("RedactURL() = %q, want %q", got, want)
	}
}

func TestRedactText(t *testing.T) {
	rawURL := "rtsp://user:secret@10.0.0.10:554/live"
	got := RedactText("failed to open "+rawURL, rawURL)
	if got == "failed to open "+rawURL {
		t.Fatal("RedactText left raw URL in place")
	}
	want := "failed to open rtsp://redacted:redacted@10.0.0.10:554/live"
	if got != want {
		t.Fatalf("RedactText() = %q, want %q", got, want)
	}
}

func TestParseFFProbePayloadIncludesCompatibilityMetadata(t *testing.T) {
	result, err := parseFFProbePayload(strings.NewReader(`{
		"format":{"format_name":"flv"},
		"streams":[
			{"index":0,"codec_type":"video","codec_name":"hevc","profile":"Main 10","level":153,"pix_fmt":"yuv420p10le","bits_per_raw_sample":"10","width":3840,"height":2160,"avg_frame_rate":"20/1"},
			{"index":1,"codec_type":"audio","codec_name":"aac"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	video := result.Streams[0]
	if result.Format != "flv" || video.Profile != "Main 10" || video.Level != "5.1" || video.PixelFormat != "yuv420p10le" || video.BitDepth != 10 {
		t.Fatalf("metadata = %#v / format %q", video, result.Format)
	}
	if video.FrameRate != "20/1" || video.FPS != 20 {
		t.Fatalf("fps metadata = %#v", video)
	}
}

func TestRedactURLMasksCredentialQueryValues(t *testing.T) {
	got := RedactURL("http://10.0.0.2/flv?port=1935&app=bcs&user=admin&password=secret&token=abc")
	for _, secret := range []string{"admin", "secret", "abc"} {
		if strings.Contains(got, secret) {
			t.Fatalf("RedactURL leaked %q in %q", secret, got)
		}
	}
	if !strings.Contains(got, "port=1935") || !strings.Contains(got, "password=redacted") {
		t.Fatalf("RedactURL query = %q", got)
	}
}

func TestParseFFProbePayloadInfersBitDepthFromPixelFormat(t *testing.T) {
	result, err := parseFFProbePayload(strings.NewReader(`{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","pix_fmt":"yuv420p","bits_per_raw_sample":"0"},{"index":1,"codec_type":"video","codec_name":"hevc","pix_fmt":"yuv420p10le"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Streams[0].BitDepth != 8 || result.Streams[1].BitDepth != 10 {
		t.Fatalf("inferred bit depths = %d/%d", result.Streams[0].BitDepth, result.Streams[1].BitDepth)
	}
}
