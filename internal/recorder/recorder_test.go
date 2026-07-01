package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildFFmpegArgsUsesLocalGo2RTCInput(t *testing.T) {
	args := BuildFFmpegArgs("rtsp://127.0.0.1:8554/cam1", "/tmp/cam1", 30)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-i rtsp://127.0.0.1:8554/cam1") {
		t.Fatalf("expected local go2rtc RTSP input, got %s", joined)
	}
	if !strings.Contains(joined, "-segment_time 1800") {
		t.Fatalf("expected 30 minute segments, got %s", joined)
	}
	if !strings.Contains(joined, "%Y-%m-%d_%H-%M.mp4") {
		t.Fatalf("expected dated strftime output pattern, got %s", joined)
	}
}

func TestParseSegmentPath(t *testing.T) {
	line := "[segment @ 0xabc] Opening '/data/temp/cam1/2026-06-30/2026-06-30_16-30.mp4' for writing"
	got := ParseSegmentPath(line)
	want := "/data/temp/cam1/2026-06-30/2026-06-30_16-30.mp4"
	if got != want {
		t.Fatalf("ParseSegmentPath() = %q, want %q", got, want)
	}
}

func TestTimestampFromSegmentPath(t *testing.T) {
	ts, ok := TimestampFromSegmentPath("/data/temp/cam1/2026-06-30/2026-06-30_16-30.mp4")
	if !ok {
		t.Fatal("expected timestamp parse to succeed")
	}
	if ts < 1782800000 || ts > 1782810000 {
		t.Fatalf("unexpected timestamp %f", ts)
	}
}

func TestMoveToRecordingsUsesCameraNameForArchivePath(t *testing.T) {
	root := t.TempDir()
	tempPath := filepath.Join(root, "temp", "cam1", "2026-06-30", "Front Gate_2026-06-30_16-30.mp4")
	if err := mkdirWrite(tempPath, []byte("video")); err != nil {
		t.Fatal(err)
	}
	final, size, err := MoveToRecordings(tempPath, "Front Gate", "cam1", filepath.Join(root, "recordings"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "recordings", "Front-Gate", "2026-06-30", "Front-Gate_2026-06-30_16-30.mp4")
	if final != want {
		t.Fatalf("final = %q, want %q", final, want)
	}
	if size == nil || *size != 5 {
		t.Fatalf("size = %v, want 5", size)
	}
}

func TestRecordingArchiveNameFallsBackToStreamName(t *testing.T) {
	got := RecordingArchiveName(" / ", "cam1")
	if got != "cam1" {
		t.Fatalf("RecordingArchiveName() = %q, want cam1", got)
	}
}

func mkdirWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
