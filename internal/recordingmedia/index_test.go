package recordingmedia

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This small functional fixture checks real muxer output with B frames, AAC,
// normal archive rotation, and a known packet epoch. No camera is contacted.
func TestFFmpegFragmentEpochAndPartialTail(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.mp4")
	command := func(args ...string) {
		t.Helper()
		out, err := exec.Command(ffmpeg, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg: %v: %s", err, out)
		}
	}
	command("-f", "lavfi", "-i", "testsrc2=size=128x72:rate=10", "-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", "6", "-c:v", "libx264", "-g", "10", "-bf", "2", "-c:a", "aac", source)
	command("-copyts", "-itsoffset", "1788912000", "-i", source, "-map", "0:v:0", "-map", "0:a?", "-c", "copy", "-f", "segment", "-segment_time", "3", "-reset_timestamps", "0", "-avoid_negative_ts", "disabled", "-segment_format_options", "movflags=+frag_keyframe+empty_moov+default_base_moof:write_prft=pts:use_editlist=1:flush_packets=1", filepath.Join(root, "out-%d.mp4"))
	files, err := filepath.Glob(filepath.Join(root, "out-*.mp4"))
	if err != nil || len(files) != 2 {
		t.Fatalf("rotation files=%v err=%v", files, err)
	}
	for i, path := range files {
		idx, err := Inspect(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(idx.Fragments) != 3 || idx.TimeBasis != TimeBasis || idx.VideoCodec != "avc1" {
			t.Fatalf("index=%+v", idx)
		}
		first := idx.Fragments[0]
		last := idx.Fragments[2]
		if first.StartMs != 1788912000000+int64(i)*3000 || last.EndMs != first.StartMs+3000 || first.MediaStartMs != 200 {
			t.Fatalf("incorrect packet/CTS mapping first=%+v last=%+v", first, last)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// An open archive prefix containing init + complete first fragment decodes.
		prefix := filepath.Join(root, "prefix.mp4")
		if err = os.WriteFile(prefix, data[:first.Offset+first.Length], 0600); err != nil {
			t.Fatal(err)
		}
		command("-i", prefix, "-f", "null", "-")
		for _, end := range []int64{last.Offset + 2, last.Offset + last.Length - 1} {
			partial, err := ReadIndex(bytes.NewReader(data), end)
			if err != nil {
				t.Fatal(err)
			}
			if len(partial.Fragments) != 2 || partial.CommittedOffset > last.Offset {
				t.Fatalf("published partial tail at %d: %+v", end, partial)
			}
		}
		// A complete mdat cannot justify a sample range that points outside it.
		bad := append([]byte(nil), data...)
		trun := bytes.Index(bad[first.Offset:first.Offset+first.Length], []byte("trun")) + int(first.Offset)
		if trun < int(first.Offset) {
			t.Fatal("missing trun")
		}
		binary.BigEndian.PutUint32(bad[trun+12:trun+16], 0x7fffffff)
		if _, err = ReadIndex(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Fatal("accepted out-of-bounds sample range")
		}
	}
}

func TestReadIndexRejectsOversizedBoxWithoutAllocation(t *testing.T) {
	data := []byte{0, 0, 0, 1, 'm', 'o', 'o', 'v', 255, 255, 255, 255, 255, 255, 255, 255}
	if _, err := ReadIndex(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("accepted overflowing box")
	}
}
