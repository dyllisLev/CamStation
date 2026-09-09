package recorder

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camstation/internal/recordingmedia"
	"camstation/internal/store"
)

// Run with production ffmpeg on PATH and CAMSTATION_TEST_GO2RTC pointing to the
// matching go2rtc binary. RTSP probing, unlike a file/TS input, exposed a leading
// timestamp-less H.264 packet on FFmpeg 5.1.7. All input here is local synthesis.
func TestRTSPRecordingPreservesEpochAndRotates(t *testing.T) {
	relayBinary := os.Getenv("CAMSTATION_TEST_GO2RTC")
	if relayBinary == "" {
		t.Skip("set CAMSTATION_TEST_GO2RTC for local RTSP recording integration")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.mp4")
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x72:rate=10", "-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", "10", "-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-bf", "0", "-c:a", "aac", source).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture: %v: %s", err, out)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	config, _ := json.Marshal(map[string]any{
		"api": map[string]string{"listen": ""}, "rtsp": map[string]string{"listen": address}, "webrtc": map[string]string{"listen": ""},
		"streams": map[string]string{"synthetic": "exec:" + quote(ffmpeg) + " -v error -re -i " + quote(source) + " -c copy -f rtsp -rtsp_transport tcp {output}"},
	})
	// The deadline bounds only this synthetic fixture and its own children.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	relay := exec.CommandContext(ctx, relayBinary, "-config", string(config))
	relayLog, err := os.Create(filepath.Join(root, "relay.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer relayLog.Close()
	relay.Stdout = relayLog
	relay.Stderr = relayLog
	if err = relay.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Process.Signal(os.Interrupt); _ = relay.Wait() }()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		conn, dialErr := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("local RTSP listener unavailable")
		case <-tick.C:
		}
	}
	args := BuildFFmpegArgsForPolicy("rtsp://"+address+"/synthetic", root, 5, "RTSP", store.CameraAudioSource)
	// Exercise normal archive rotation without waiting for a five-minute file.
	for i := 1; i < len(args); i++ {
		if args[i] == "-segment_time" {
			args[i+1] = "2"
		}
	}
	recorder := exec.CommandContext(ctx, ffmpeg, args[1:]...)
	log, err := os.Create(filepath.Join(root, "recorder.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	recorder.Stderr = log
	stdin, err := recorder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err = recorder.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() error {
		_, _ = stdin.Write([]byte("q\n"))
		_ = stdin.Close()
		err := recorder.Wait()
		stopped = true
		return err
	}
	defer func() {
		if !stopped {
			_ = stop()
		}
	}()
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	var complete []recordingmedia.Index
	var decodePath string
loop:
	for {
		select {
		case <-deadline.C:
			t.Fatal("RTSP recording did not publish and rotate within the fixture")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
			files, _ := filepath.Glob(filepath.Join(root, "RTSP_*.mp4"))
			complete = nil
			for _, path := range files {
				idx, indexErr := recordingmedia.Inspect(path)
				if indexErr != nil {
					t.Fatalf("RTSP input lost packet time: %v", indexErr)
				}
				if len(idx.Fragments) > 0 {
					complete = append(complete, idx)
					decodePath = path
				}
			}
			if len(complete) >= 2 {
				count := 0
				for _, idx := range complete {
					count += len(idx.Fragments)
				}
				if count >= 3 {
					break loop
				}
			}
		}
	}
	for _, idx := range complete {
		first, last := idx.Fragments[0], idx.Fragments[len(idx.Fragments)-1]
		if first.StartMs < started.UnixMilli() || last.EndMs > time.Now().Add(time.Second).UnixMilli() {
			t.Fatalf("invalid packet epoch: %+v", idx)
		}
		if last.MediaEndMs-first.MediaStartMs < 500 {
			t.Fatalf("RTSP video duration collapsed: %+v", idx)
		}
	}
	// Decode only the committed prefix while the writer is still running.
	idx, err := recordingmedia.Inspect(decodePath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(decodePath)
	if err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "prefix.mp4")
	if err = os.WriteFile(prefix, data[:idx.CommittedOffset], 0600); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(ffmpeg, "-v", "error", "-i", prefix, "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("RTSP committed-prefix decode: %v: %s", err, out)
	}
	if err = stop(); err != nil {
		t.Fatal(fmt.Errorf("graceful recorder stop: %w", err))
	}
}
