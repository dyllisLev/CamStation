package recordingmedia

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
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
	command("-copyts", "-itsoffset", "1788912000", "-i", source, "-map", "0:v:0", "-map", "0:a?", "-c", "copy", "-f", "segment", "-segment_frames", "30", "-reset_timestamps", "0", "-avoid_negative_ts", "disabled", "-segment_format_options", "movflags=+frag_keyframe+empty_moov+default_base_moof+frag_discont:write_prft=pts:use_editlist=0:avoid_negative_ts=disabled:flush_packets=1", filepath.Join(root, "out-%d.mp4"))
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

// Waiting for an RTSP keyframe may leave audio ahead of video. Their common
// decode clock must survive rotation; video wall-clock time still comes from PRFT.
func TestFFmpegFragmentPreservesAudioLead(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	root := t.TempDir()
	video, audio := filepath.Join(root, "video.mp4"), filepath.Join(root, "audio.m4a")
	command := func(args ...string) {
		t.Helper()
		out, err := exec.Command(ffmpeg, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg: %v: %s", err, out)
		}
	}
	command("-f", "lavfi", "-i", "testsrc2=size=128x72:rate=10", "-t", "4", "-c:v", "libx264", "-g", "10", "-bf", "0", video)
	command("-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", "6", "-c:a", "aac", audio)
	for _, legacy := range []bool{false, true} {
		name, flags := "common", "movflags=+frag_keyframe+empty_moov+default_base_moof+frag_discont:write_prft=pts:use_editlist=0:avoid_negative_ts=disabled:flush_packets=1"
		if legacy {
			name, flags = "legacy", "movflags=+frag_keyframe+empty_moov+default_base_moof:write_prft=pts:use_editlist=1:flush_packets=1"
		}
		command("-copyts", "-itsoffset", "1788912002", "-i", video, "-itsoffset", "1788912000", "-i", audio, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-f", "segment", "-segment_frames", "20", "-reset_timestamps", "0", "-avoid_negative_ts", "disabled", "-segment_format_options", flags, filepath.Join(root, name+"-%d.mp4"))
		for i := 0; i < 2; i++ {
			path := filepath.Join(root, fmt.Sprintf("%s-%d.mp4", name, i))
			idx, err := Inspect(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(idx.Fragments) != 2 {
				t.Fatalf("%s: fragments=%+v", path, idx.Fragments)
			}
			first := idx.Fragments[0]
			if first.StartMs != 1788912002000+int64(i)*2000 {
				t.Fatalf("%s: lost epoch: %+v", path, first)
			}
			if !legacy && i == 0 && (first.MediaStartMs < 2021 || first.MediaStartMs > 2022) {
				t.Fatalf("lost two-second audio lead: %+v", first)
			}
			if legacy && first.MediaStartMs != 0 {
				t.Fatalf("legacy timeline changed: %+v", first)
			}
			if idx.Fragments[1].MediaStartMs-first.MediaStartMs != 1000 {
				t.Fatalf("unstable normalized clock: %+v", idx.Fragments)
			}
		}
	}
}

// These unmodified moof boxes came from consecutive FFmpeg 5.1 AAC/H264
// fragments. At the boundary two AAC packets have the same DTS: the first
// sample in fragment 47 has duration 0 and a real 466-byte payload. No camera
// addresses, media payload, or stream labels are part of this fixture.
func TestReadRunsAcceptsAACPacketWithRepeatedDTS(t *testing.T) {
	encoded := []string{
		"AAACHG1vb2YAAAAQbWZoZAAAAAAAAAAvAAABQHRyYWYAAAAcdGZoZAACADgAAAABAAAdEQACeM4BAQAAAAAAFHRmZHQBAAAAAAAY/dNuw84AAAEIdHJ1bgAAAwUAAAAeAAACJAIAAAAAAB0RAAJ4zgAAAAsAABPlAAAAGAAAFM4AAAnMAAAUywAAAAEAABWwAAAEUwAAFgkAAAACAAAWawAACf0AABaBAAAAAgAAFoIAAAACAAAV5AAAAAEAABbyAAAAAgAAFxYAAAABAAAXcwAAAl8AABcvAAAEYQAAF5QAAAPQAAAXJAAABAAAABdUAAAEIAAAFyYAAAU6AAAVpwAAApwAABajAAAD/wAAFhwAAAetAAAWmgAAAD8AABYyAAAJZgAAFogAAAACAAAWGQAAAs4AABV0AAAD0gAAFhYAAAQ9AAAV/gAABIQAABXaAAADiQAAFb8AAADEdHJhZgAAABx0ZmhkAAIAOAAAAAIAAAAAAAAB0gIAAAAAAAAUdGZkdAEAAAAAAA0EM3RdSQAAAIx0cnVuAAADAQAAAA8ABQBLAAAAAAAAAdIAAAEzAAABxAAAAAwAAAHSAAAEfwAAAcUAAAT/AAAB1QAAAnUAAAHgAAAFGgAAAcwAAAJPAAAB3wAACqoAAAHQAAACEgAAAdgAAAQpAAAB6AAABCoAAAHXAAACOwAAAeEAAAQIAAAB2AAABAAAAAHO",
		"AAACHG1vb2YAAAAQbWZoZAAAAAAAAAAwAAABQHRyYWYAAAAcdGZoZAACADgAAAABAAAGNgACeT4BAQAAAAAAFHRmZHQBAAAAAAAY/dNvO+wAAAEIdHJ1bgAAAwUAAAAeAAACJAIAAAAAAAY2AAJ5PgAAA5cAABROAAAOswAAFbUAAAACAAAUlAAACWUAABV2AAAABAAAFoYAAATgAAAWqwAAAAIAABdQAAAJrQAAF6EAAAADAAAXxwAAAAIAABcPAAAAAgAAFycAAAW1AAAXbwAAAccAABcNAAAD2QAAFt8AAAP7AAAVewAAA/4AABYHAAAD7AAAF5oAAARhAAAWpAAAA/sAABauAAADqwAAFgoAAAbSAAAWlgAAAT4AABaFAAAERQAAFq0AAAO0AAAXbQAABCIAABXkAAAGgQAAFvMAAAN2AAAVngAAAtgAABY3AAADZAAAFt8AAADEdHJhZgAAABx0ZmhkAAIAOAAAAAIAAAy3AAAB0QIAAAAAAAAUdGZkdAEAAAAAAA0EM3SRNgAAAIx0cnVuAAADAQAAAA8ABQghAAAMtwAAAdEAAAAMAAAB2AAAABAAAAHOAAAB8wAAAdcAAARNAAAB0wAAAlsAAAHVAAAE6gAAAdwAAAJxAAAB1AAADzMAAAHXAAAEQAAAAdAAAAIRAAABwgAABDkAAAHaAAAEIQAAAcYAAAQYAAAB2AAABAAAAAHZ",
	}
	tracks := map[uint32]track{1: {id: 1, scale: 15360, video: true, codec: "avc1"}, 2: {id: 2, scale: 8000, codec: "mp4a"}}
	var previousEnd int64
	for i, fixture := range encoded {
		data, err := base64.StdEncoding.DecodeString(fixture)
		if err != nil {
			t.Fatal(err)
		}
		runs, seq, err := readRuns(data[8:], tracks, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if seq != int64(47+i) || len(runs) != 2 {
			t.Fatalf("seq=%d runs=%+v", seq, runs)
		}
		audio := runs[1]
		if i == 0 && (audio.first != 14311694294345 || audio.decodeEnd != 14311694307638) {
			t.Fatalf("unexpected audio interval: %+v", audio)
		}
		if i > 0 && audio.first != previousEnd {
			t.Fatalf("AAC decode clock lost continuity: %+v", audio)
		}
		previousEnd = audio.decodeEnd
		if i != 0 {
			continue
		}
		// An actual zero-byte sample remains invalid even with a repeated DTS.
		bad := append([]byte(nil), data...)
		trun := bytes.LastIndex(bad, []byte("trun"))
		binary.BigEndian.PutUint32(bad[trun+20:trun+24], 0)
		if _, _, err := readRuns(bad[8:], tracks, 1000); err == nil {
			t.Fatal("accepted zero-size AAC sample")
		}
		// The exception is specific to the observed AAC boundary, not video.
		changed := map[uint32]track{1: tracks[1], 2: {id: 2, scale: 8000, video: true, codec: "avc1"}}
		if _, _, err := readRuns(data[8:], changed, 1000); err == nil {
			t.Fatal("accepted zero-duration video sample")
		}
	}
}
