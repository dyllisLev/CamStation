package camera

import "testing"

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
