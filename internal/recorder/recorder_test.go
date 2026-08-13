package recorder

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"camstation/internal/opslog"
	"camstation/internal/store"
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
	if !strings.Contains(joined, "-stats_period 60 -progress pipe:1") {
		t.Fatalf("expected bounded ffmpeg progress output, got %s", joined)
	}
}

func TestBuildFFmpegArgsUsesWallclockPtsForStableSegmentation(t *testing.T) {
	args := BuildFFmpegArgs("rtsp://127.0.0.1:8554/cam1", "/tmp/cam1", 30)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-fflags +genpts") {
		t.Fatalf("expected generated PTS for stable MP4 playback, got %s", joined)
	}
	if !strings.Contains(joined, "-use_wallclock_as_timestamps 1 -rtsp_transport tcp -i") {
		t.Fatalf("expected wallclock input timestamps before RTSP input, got %s", joined)
	}
}

func TestRecorderProgressLogsFirstMediaThenDebugProgress(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	worker := &worker{camera: store.Camera{ID: 7, StreamName: "gate-recording"}, manager: &Manager{logger: logger}}
	progress := strings.NewReader("frame=30\nout_time_us=2000000\nprogress=continue\nframe=900\nout_time_us=60000000\nprogress=continue\n")
	if err := worker.watchProgress(progress, 2); err != nil {
		t.Fatal(err)
	}
	records := decodeRecorderOperationalLines(t, output.String())
	if len(records) != 2 || records[0].Event != "media_started" || records[0].Level != "info" ||
		records[0].CameraID != 7 || records[0].StreamName != "gate-recording" || records[0].Attempt != 2 ||
		records[0].Frame != 30 || records[0].MediaTimeMS != 2000 ||
		records[1].Event != "media_progress" || records[1].Level != "debug" || records[1].Frame != 900 {
		t.Fatalf("records=%+v", records)
	}
}

func TestRecorderFFmpegWarningIsRedactedAndClassified(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	worker := &worker{camera: store.Camera{ID: 7, StreamName: "gate-recording"}, manager: &Manager{logger: logger}}
	worker.logFFmpegLine(`Connection timed out for rtsp://admin:secret@10.0.0.2/live?token=bad at /var/lib/camstation/media/temp/file.mp4`, 3)
	records := decodeRecorderOperationalLines(t, output.String())
	if len(records) != 1 || records[0].Event != "ffmpeg_warning" || records[0].Level != "warn" || records[0].Attempt != 3 {
		t.Fatalf("records=%+v", records)
	}
	lower := strings.ToLower(output.String())
	for _, forbidden := range []string{"admin", "secret", "10.0.0.2", "token=", "rtsp://", "/var/lib"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("recorder log leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestRecorderFFmpegRepeatedOutputIsRateLimited(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	worker := &worker{camera: store.Camera{ID: 7, StreamName: "gate-recording"}, manager: &Manager{logger: logger}}
	for index := 0; index < 1000; index++ {
		worker.logFFmpegLine("DTS "+strconv.Itoa(index+10)+", next:"+strconv.Itoa(index+20)+" st:0 invalid dropping", 3)
	}
	records := decodeRecorderOperationalLines(t, output.String())
	if len(records) != 1 || records[0].Event != "ffmpeg_error" || records[0].MessageFingerprint == "" {
		t.Fatalf("records=%+v", records)
	}
}

func decodeRecorderOperationalLines(t *testing.T, value string) []opslog.Record {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(value), "\n")
	result := make([]opslog.Record, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var record opslog.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log %q: %v", line, err)
		}
		result = append(result, record)
	}
	return result
}

func TestBuildFFmpegArgsResetsAudioPtsForSegmentPlayback(t *testing.T) {
	args := BuildFFmpegArgs("rtsp://127.0.0.1:8554/cam1", "/tmp/cam1", 5)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-af asetpts=PTS-STARTPTS") {
		t.Fatalf("expected audio PTS reset for stable MP4 duration, got %s", joined)
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
