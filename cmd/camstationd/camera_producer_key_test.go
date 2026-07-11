package main

import (
	"encoding/json"
	"strings"
	"testing"

	"camstation/internal/cameraprofile"
)

func TestRedactDeviceScanResultAssignsOpaqueProducerKeysByExactRawURL(t *testing.T) {
	first := "http://192.168.1.10/flv?user=admin&token=first-secret"
	second := "http://192.168.1.10/flv?user=admin&token=second-secret"
	scan := cameraprofile.DeviceScanResult{Channels: []cameraprofile.ScanChannel{
		{Index: 0, Candidates: []cameraprofile.StreamCandidate{{URL: first}, {URL: second}}},
		{Index: 1, Candidates: []cameraprofile.StreamCandidate{{URL: first}}},
	}}

	public := redactDeviceScanResult(scan)
	one := public.Channels[0].Candidates[0]
	two := public.Channels[0].Candidates[1]
	repeated := public.Channels[1].Candidates[0]
	if one.ProducerKey == "" || two.ProducerKey == "" || one.ProducerKey == two.ProducerKey || repeated.ProducerKey != one.ProducerKey {
		t.Fatalf("producer keys = %q/%q/%q", one.ProducerKey, two.ProducerKey, repeated.ProducerKey)
	}
	if one.RedactedURL != two.RedactedURL {
		t.Fatalf("test requires credential-only URL difference to collapse after redaction: %q/%q", one.RedactedURL, two.RedactedURL)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"first-secret", "second-secret", `"url":`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public scan leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestRedactDeviceProfileUsesSameProducerKeyContract(t *testing.T) {
	raw := "rtsp://admin:secret@192.168.1.10/main"
	profile := cameraprofile.DeviceProfile{Channels: []cameraprofile.ChannelProfile{{Candidates: []cameraprofile.StreamCandidate{{URL: raw}, {URL: raw}}}}}
	public := redactDeviceProfile(profile)
	first, second := public.Channels[0].Candidates[0], public.Channels[0].Candidates[1]
	if first.ProducerKey == "" || first.ProducerKey != second.ProducerKey || first.URL != "" || second.URL != "" {
		t.Fatalf("profile producer identity = %#v/%#v", first, second)
	}
}
