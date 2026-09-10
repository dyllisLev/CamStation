package recordingmedia

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

type flatProbe struct {
	Streams []struct {
		Index    int
		TimeBase string `json:"time_base"`
	}
	Packets []struct {
		Stream   int `json:"stream_index"`
		PTS      int64
		DTS      int64
		Duration int64
		Hash     string `json:"data_hash"`
	}
	Format struct{ Duration string }
}

func probeFlatFile(t *testing.T, path string) flatProbe {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-show_data_hash", "sha256", "-show_entries", "stream=index,time_base:packet=stream_index,pts,dts,duration,data_hash:format=duration", "-of", "json", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe: %v %s", err, out)
	}
	var p flatProbe
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func (p flatProbe) unit(stream int) float64 {
	for _, s := range p.Streams {
		if s.Index == stream {
			var n, d int64
			fmt.Sscanf(s.TimeBase, "%d/%d", &n, &d)
			return float64(n) / float64(d)
		}
	}
	return 0
}

// These are small generated sources, not cameras. Expected timeline/payload is
// derived from the actual source packets, independently of the view's parser.
func TestFlatMP4DurationPayloadAndAVEdits(t *testing.T) {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " unavailable")
		}
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg: %v %s", err, out)
		}
	}
	video := func(name, seconds, bframes string) string {
		p := filepath.Join(dir, name)
		run("-f", "lavfi", "-i", "testsrc2=size=64x64:rate=10", "-t", seconds, "-c:v", "libx264", "-g", "10", "-bf", bframes, p)
		return p
	}
	audio := func(name, seconds string) string {
		p := filepath.Join(dir, name)
		run("-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", seconds, "-c:a", "aac", p)
		return p
	}
	v2, v4, b4 := video("video2.mp4", "2", "0"), video("video4.mp4", "4", "0"), video("bframes4.mp4", "4", "2")
	a2, a4 := audio("audio2.m4a", "2"), audio("audio4.m4a", "4")
	for _, flags := range []string{"+frag_keyframe+empty_moov", "+frag_keyframe+empty_moov+default_base_moof"} {
		t.Run("legacy-"+flags, func(t *testing.T) {
			path := filepath.Join(dir, "legacy.mp4")
			run("-i", b4, "-c", "copy", "-movflags", flags, "-write_prft", "pts", path)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			view, err := NormalizedMP4(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			out, err := io.ReadAll(view)
			if err != nil || !bytes.Equal(out, raw) {
				t.Fatal("relative legacy fragment changed", err)
			}
		})
	}
	for _, tc := range []struct {
		name, video, audio     string
		videoDelay, audioDelay int
		negativeCTS            bool
	}{
		{name: "audio-leading", video: v2, audio: a4, videoDelay: 2},
		{name: "video-leading", video: v4, audio: a2, audioDelay: 2},
		{name: "b-frames", video: b4, audio: a4},
		{name: "no-audio", video: b4},
		{name: "signed-composition-times", video: b4, audio: a4, negativeCTS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := filepath.Join(dir, tc.name+"-raw.mp4")
			flat := filepath.Join(dir, tc.name+"-flat.mp4")
			args := []string{"-copyts", "-itsoffset", strconv.Itoa(1788912000 + tc.videoDelay), "-i", tc.video}
			if tc.audio != "" {
				args = append(args, "-itsoffset", strconv.Itoa(1788912000+tc.audioDelay), "-i", tc.audio)
			}
			args = append(args, "-map", "0:v:0")
			if tc.audio != "" {
				args = append(args, "-map", "1:a:0")
			}
			flags := "+frag_keyframe+empty_moov+default_base_moof+frag_discont"
			if tc.negativeCTS {
				flags += "+negative_cts_offsets"
			}
			args = append(args, "-c", "copy", "-avoid_negative_ts", "disabled", "-movflags", flags, "-write_prft", "pts", "-use_editlist", "0", raw)
			run(args...)
			canonical, err := os.ReadFile(raw)
			if err != nil {
				t.Fatal(err)
			}
			view, err := NormalizedMP4(bytes.NewReader(canonical), int64(len(canonical)))
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(view)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(flat, data, 0600); err != nil {
				t.Fatal(err)
			}
			before, after := probeFlatFile(t, raw), probeFlatFile(t, flat)
			sources := []flatProbe{probeFlatFile(t, tc.video)}
			delays := []float64{float64(tc.videoDelay)}
			if tc.audio != "" {
				sources = append(sources, probeFlatFile(t, tc.audio))
				delays = append(delays, float64(tc.audioDelay))
			}
			if len(before.Packets) == 0 || len(before.Packets) != len(after.Packets) {
				t.Fatalf("packet count %d -> %d", len(before.Packets), len(after.Packets))
			}
			origin := math.Inf(1)
			for i, source := range sources {
				for _, p := range source.Packets {
					origin = math.Min(origin, float64(p.PTS)*source.unit(p.Stream)+delays[i])
				}
			}
			duration, err := strconv.ParseFloat(after.Format.Duration, 64)
			if err != nil {
				t.Fatal(err)
			}
			// Inputs all end at exactly epoch+4s. AAC starts one encoder
			// priming packet earlier; its last shortened packet ends at that EOS.
			// ffprobe guesses the regular codec packet duration for the short tail,
			// so summing its duration fields would overstate this ground truth.
			wantDuration := 4 - origin
			if math.Abs(duration-wantDuration) > 0.001 {
				t.Fatalf("duration %.6f want source EOS %.6f", duration, wantDuration)
			}
			// Different track chunks can interleave differently in ffprobe's iterator;
			// compare the packet sequence within each track rather than global ordering.
			for _, stream := range before.Streams {
				var oldPackets, newPackets []int
				for i, p := range before.Packets {
					if p.Stream == stream.Index {
						oldPackets = append(oldPackets, i)
					}
				}
				for i, p := range after.Packets {
					if p.Stream == stream.Index {
						newPackets = append(newPackets, i)
					}
				}
				if len(oldPackets) != len(newPackets) {
					t.Fatal("track sample count changed")
				}
				for n, oldIndex := range oldPackets {
					a, b := before.Packets[oldIndex], after.Packets[newPackets[n]]
					if a.Hash == "" || a.Hash != b.Hash {
						t.Fatalf("track %d sample %d payload changed", stream.Index, n)
					}
					source := sources[stream.Index]
					if n >= len(source.Packets) {
						t.Fatal("more output samples than input")
					}
					input := source.Packets[n]
					unit := source.unit(input.Stream)
					want := float64(input.PTS)*unit + delays[stream.Index] - origin
					got := float64(b.PTS) * after.unit(stream.Index)
					if math.Abs(got-want) > unit+0.000002 {
						t.Fatalf("track %d sample %d AV time %.6f want %.6f", stream.Index, n, got, want)
					}
				}
			}
			untouched, err := os.ReadFile(raw)
			if err != nil || !bytes.Equal(untouched, canonical) {
				t.Fatal("canonical file changed", err)
			}
			second, err := NormalizedMP4(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			again, _ := io.ReadAll(second)
			if !bytes.Equal(again, data) {
				t.Fatal("ordinary flat MP4 did not pass through")
			}
			run("-i", flat, "-f", "null", "-")
		})
	}
}

// Reuses the observed fragment-47 metadata, with synthetic payload bytes only.
func TestFlatMP4PreservesZeroDurationAACSample(t *testing.T) {
	moof, err := base64.StdEncoding.DecodeString("AAACHG1vb2YAAAAQbWZoZAAAAAAAAAAvAAABQHRyYWYAAAAcdGZoZAACADgAAAABAAAdEQACeM4BAQAAAAAAFHRmZHQBAAAAAAAY/dNuw84AAAEIdHJ1bgAAAwUAAAAeAAACJAIAAAAAAB0RAAJ4zgAAAAsAABPlAAAAGAAAFM4AAAnMAAAUywAAAAEAABWwAAAEUwAAFgkAAAACAAAWawAACf0AABaBAAAAAgAAFoIAAAACAAAV5AAAAAEAABbyAAAAAgAAFxYAAAABAAAXcwAAAl8AABcvAAAEYQAAF5QAAAPQAAAXJAAABAAAABdUAAAEIAAAFyYAAAU6AAAVpwAAApwAABajAAAD/wAAFhwAAAetAAAWmgAAAD8AABYyAAAJZgAAFogAAAACAAAWGQAAAs4AABV0AAAD0gAAFhYAAAQ9AAAV/gAABIQAABXaAAADiQAAFb8AAADEdHJhZgAAABx0ZmhkAAIAOAAAAAIAAAAAAAAB0gIAAAAAAAAUdGZkdAEAAAAAAA0EM3RdSQAAAIx0cnVuAAADAQAAAA8ABQBLAAAAAAAAAdIAAAEzAAABxAAAAAwAAAHSAAAEfwAAAcUAAAT/AAAB1QAAAnUAAAHgAAAFGgAAAcwAAAJPAAAB3wAACqoAAAHQAAACEgAAAdgAAAQpAAAB6AAABCoAAAHXAAACOwAAAeEAAAQIAAAB2AAABAAAAAHO")
	if err != nil {
		t.Fatal(err)
	}
	tracks := map[uint32]track{1: {id: 1, scale: 15360, video: true, codec: "avc1"}, 2: {id: 2, scale: 8000, codec: "mp4a"}}
	runs, _, err := readRuns(moof[8:], tracks, 0)
	if err != nil {
		t.Fatal(err)
	}
	var end int64
	for _, r := range runs {
		for _, span := range r.ranges {
			end = max(end, span.end)
		}
	}
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	trex := func(id uint32) []byte {
		return normBox("trex", make([]byte, 4), normU32(id), normU32(1), make([]byte, 12))
	}
	init := normBox("moov", normBox("mvhd", mvhd), normTrack(1, 15360), normTrack(2, 8000), normBox("mvex", trex(1), trex(2)))
	media := bytes.Repeat([]byte{0x5a}, int(end)-len(moof)-8)
	prft := normBox("prft", []byte{1, 0, 0, 0}, normU32(1), normU64(0xed00000012345678), normU64(uint64(runs[0].first)))
	raw := bytes.Join([][]byte{normBox("ftyp", []byte("isom0000")), init, prft, moof, normBox("mdat", media)}, nil)
	view, err := NormalizedMP4(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(view)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.LastIndex(out, []byte("stts"))
	if at < 0 || binary.BigEndian.Uint32(out[at+12:]) != 1 || binary.BigEndian.Uint32(out[at+16:]) != 0 {
		t.Fatal("zero duration AAC sample lost from stts")
	}
	at = bytes.LastIndex(out, []byte("stsz"))
	if at < 0 || binary.BigEndian.Uint32(out[at+12:]) != 15 || binary.BigEndian.Uint32(out[at+16:]) != 466 {
		t.Fatal("AAC sample count or zero-duration payload size changed")
	}
	at = bytes.LastIndex(out, []byte("mdat"))
	if at < 0 || !bytes.Equal(out[at+4:], media) {
		t.Fatal("AAC payload changed")
	}
}
