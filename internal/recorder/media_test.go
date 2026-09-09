package recorder

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"camstation/internal/recordingmedia"
	"camstation/internal/store"
)

func TestGrowingRecordingPublishesPacketTimeAndFinalizesSameMedia(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.mp4")
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x72:rate=10", "-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", "8", "-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-bf", "0", "-c:a", "aac", source).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture: %v: %s", err, out)
	}
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	tempDir := filepath.Join(root, "temp")
	if err = os.MkdirAll(tempDir, 0700); err != nil {
		t.Fatal(err)
	}
	manager := New(db, filepath.Join(root, "archive"), tempDir, 5)
	w := &worker{camera: store.Camera{ID: 1, Name: "Test", StreamName: "test"}, manager: manager}
	args := BuildFFmpegArgsForPolicy("pipe:0", tempDir, 5, "Test", store.CameraAudioSource)
	local := []string{"-probesize", "32768", "-analyzeduration", "0"}
	for i := 1; i < len(args); i++ {
		if args[i] == "-rtsp_transport" {
			i++
			continue
		}
		local = append(local, args[i])
	}
	cmd := exec.Command(ffmpeg, local...)
	var output bytes.Buffer
	cmd.Stderr = &output
	// Pace a finite local TS producer, without applying FFmpeg readrate to
	// epoch input timestamps (which makes its synthetic-input clock sleep).
	producer := exec.Command(ffmpeg, "-v", "error", "-re", "-i", source, "-c", "copy", "-f", "mpegts", "pipe:1")
	pipe, err := producer.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = pipe
	started := time.Now()
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = producer.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			<-done
		}
		_ = producer.Wait()
	}()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	var ref *segmentRef
	var published recordingmedia.Index
loop:
	for {
		select {
		case <-deadline.C:
			t.Fatal("no complete fragment appeared before archive close")
		case err = <-done:
			stopped = true
			t.Fatalf("recorder exited before recent media was published: %v: %s", err, output.String())
		case <-tick.C:
			if ref == nil {
				files, _ := filepath.Glob(filepath.Join(tempDir, "*.mp4"))
				if len(files) == 0 {
					continue
				}
				if err = w.openSegment(files[0]); err != nil {
					t.Fatal(err)
				}
				ref = w.currentRef()
			}
			idx, indexErr := w.publishSegment(ref)
			if indexErr != nil {
				t.Fatal(indexErr)
			}
			if len(idx.Fragments) > 0 {
				published = idx
				break loop
			}
		}
	}
	first := published.Fragments[0]
	if first.StartMs < started.UnixMilli()-1000 || first.StartMs > time.Now().UnixMilli() {
		t.Fatalf("packet epoch outside local reception interval: %+v", first)
	}
	segment, idx, err := db.RecordingMedia(context.Background(), ref.id)
	if err != nil || segment.Status != "recording" || len(idx.Fragments) == 0 {
		t.Fatalf("unpublished growing media: %+v %+v %v", segment, idx, err)
	}
	// Decode only bytes already committed while the ffmpeg writer is still open.
	data, err := os.ReadFile(ref.path)
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "prefix.mp4")
	if err = os.WriteFile(prefix, data[:idx.CommittedOffset], 0600); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(ffmpeg, "-v", "error", "-i", prefix, "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("open archive prefix decode: %v %s", err, out)
	}
	err = <-done
	stopped = true
	if err != nil {
		t.Fatalf("normal recorder stop: %v %s", err, output.String())
	}
	w.closeCurrent(time.Now().Unix())
	final, finalIndex, err := db.RecordingMedia(context.Background(), ref.id)
	if err != nil || final.Status != "ready" || final.BackupState != "pending" || final.FinalPath == "" || len(finalIndex.Fragments) < len(idx.Fragments) {
		t.Fatalf("finalization changed identity or lost media: %+v %+v %v", final, finalIndex, err)
	}
	if _, err = os.Stat(final.FinalPath); err != nil {
		t.Fatal(err)
	}
	if final.TSStart != float64(first.StartMs)/1000 {
		t.Fatalf("finalization lost packet time: %+v", final)
	}
}

func TestFinalizedAudioOnlyAttemptIsPreservedWithoutPlaybackCoverage(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	tempDir := filepath.Join(root, "temp")
	if err = os.MkdirAll(tempDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tempDir, "Attempt_2026-09-09_13-00-00_deadbeef.mp4")
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", "0.1", "-c:a", "aac", "-movflags", "+frag_keyframe+empty_moov+default_base_moof", path).CombinedOutput()
	if err != nil {
		t.Fatalf("audio-only fixture: %v %s", err, out)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, filepath.Join(root, "archive"), tempDir, 5)
	w := &worker{camera: store.Camera{ID: 1, Name: "Attempt", StreamName: "attempt"}, manager: manager}
	if err = w.openSegment(path); err != nil {
		t.Fatal(err)
	}
	id := w.currentRef().id
	w.closeCurrent(time.Now().Unix())
	segment, err := db.GetRecordingSegmentByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if segment.Status != "failed" || segment.Error != "no complete playable video fragment" || segment.FinalPath == "" || segment.FileSize == nil || *segment.FileSize != int64(len(original)) || segment.BackupState != "pending" {
		t.Fatalf("failed input promoted or lost: %+v", segment)
	}
	archived, err := os.ReadFile(segment.FinalPath)
	if err != nil || !bytes.Equal(archived, original) {
		t.Fatalf("failed attempt bytes lost: %v", err)
	}
	spans, err := db.PlaybackSpans(t.Context(), 1, 0, time.Now().Add(time.Hour).UnixMilli())
	if err != nil || len(spans) != 0 {
		t.Fatalf("failed attempt advertised as video: %+v %v", spans, err)
	}
}
