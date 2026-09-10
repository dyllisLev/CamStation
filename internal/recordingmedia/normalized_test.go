package recordingmedia

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func normBox(kind string, p ...[]byte) []byte {
	data := bytes.Join(p, nil)
	out := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	copy(out[4:], kind)
	copy(out[8:], data)
	return out
}
func normU32(v uint32) []byte { p := make([]byte, 4); binary.BigEndian.PutUint32(p, v); return p }
func normU64(v uint64) []byte { p := make([]byte, 8); binary.BigEndian.PutUint64(p, v); return p }
func normTrack(id, scale uint32) []byte {
	tk := make([]byte, 84)
	binary.BigEndian.PutUint32(tk[12:], id)
	md := make([]byte, 24)
	binary.BigEndian.PutUint32(md[12:], scale)
	handler := make([]byte, 12)
	codec := "avc1"
	copy(handler[8:], "vide")
	if id == 2 {
		codec = "mp4a"
		copy(handler[8:], "soun")
	}
	stbl := normBox("stbl", normBox("stsd", make([]byte, 4), normU32(1), normBox(codec)), normBox("stts", make([]byte, 8)), normBox("stsc", make([]byte, 8)), normBox("stsz", make([]byte, 12)), normBox("stco", make([]byte, 8)))
	return normBox("trak", normBox("tkhd", tk), normBox("mdia", normBox("mdhd", md), normBox("hdlr", handler), normBox("minf", stbl)))
}
func normFixture(epoch uint64) []byte {
	video := epoch*90000 + 181920
	audio := epoch * 48000
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	trex := func(id, scale uint32) []byte {
		return normBox("trex", make([]byte, 4), normU32(id), normU32(1), normU32(scale), normU32(4), normU32(0))
	}
	moov := normBox("moov", normBox("mvhd", mvhd), normTrack(1, 90000), normTrack(2, 48000), normBox("mvex", trex(1, 90000), trex(2, 48000)))
	traf := func(id uint32, n uint64, offset uint32) []byte {
		return normBox("traf", normBox("tfhd", []byte{0, 2, 0, 0}, normU32(id)), normBox("tfdt", []byte{1, 0, 0, 0}, normU64(n)), normBox("trun", []byte{0, 0, 0, 1}, normU32(1), normU32(offset)))
	}
	moof := normBox("moof", normBox("mfhd", make([]byte, 4), normU32(1)), traf(1, video, 0), traf(2, audio, 0))
	moof = normBox("moof", normBox("mfhd", make([]byte, 4), normU32(1)), traf(1, video, uint32(len(moof)+8)), traf(2, audio, uint32(len(moof)+12)))
	prft := normBox("prft", []byte{1, 0, 0, 0}, normU32(1), normU64(0xed00000012345678), normU64(video))
	tfra := normBox("tfra", []byte{1, 0, 0, 0}, normU32(1), normU32(0), normU32(1), normU64(video), normU64(987), []byte{1, 1, 1})
	sidx := normBox("sidx", []byte{1, 0, 0, 0}, normU32(1), normU32(90000), normU64(video), normU64(0), make([]byte, 4))
	return bytes.Join([][]byte{normBox("ftyp", []byte("isom0000")), moov, sidx, prft, moof, normBox("mdat", []byte("V123A456")), normBox("mfra", tfra)}, nil)
}
func TestNormalizedMP4CommonClockAndImmutablePayload(t *testing.T) {
	original := normFixture(1788912000)
	saved := bytes.Clone(original)
	view, err := NormalizedMP4(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(view)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, saved) {
		t.Fatal("canonical source changed")
	}
	if bytes.Contains(out, []byte("mvex")) {
		t.Fatal("virtual normal MP4 still declares fragments")
	}
	// Movie-scale delay is explicit; sample decode/presentation relationships
	// are conveyed by sample tables and an edit, not an epoch-valued tfdt.
	elst := bytes.Index(out, []byte("elst"))
	if elst < 0 {
		t.Fatal("missing edit list")
	}
	if got := binary.BigEndian.Uint64(out[elst+12:]); got != 2021334 {
		t.Fatalf("video delay=%d", got)
	}
	prft := bytes.Index(out, []byte("prft"))
	oldPrft := bytes.Index(original, []byte("prft"))
	if !bytes.Equal(out[prft+12:prft+20], original[oldPrft+12:oldPrft+20]) {
		t.Fatal("absolute NTP changed")
	}
	if binary.BigEndian.Uint64(out[prft+20:]) != 0 {
		t.Fatal("PRFT media_time not mapped to flat track clock")
	}
	mdat := bytes.LastIndex(out, []byte("mdat"))
	if string(out[mdat+4:mdat+12]) != "V123A456" {
		t.Fatal("sample payload changed")
	}
	co64 := bytes.Index(out, []byte("co64"))
	videoOffset := binary.BigEndian.Uint64(out[co64+12:])
	if string(out[videoOffset:videoOffset+4]) != "V123" {
		t.Fatal("flat video chunk offset incorrect")
	}
	co64 = bytes.Index(out[co64+4:], []byte("co64")) + co64 + 4
	audioOffset := binary.BigEndian.Uint64(out[co64+12:])
	if string(out[audioOffset:audioOffset+4]) != "A456" {
		t.Fatal("flat audio chunk offset incorrect")
	}
	// Range crosses the synthesized prefix/source boundary, then reads a sample.
	boundary := bytes.Index(out, []byte("mdat")) + 12
	for _, start := range []int{boundary - 3, mdat + 5} {
		stop := start + 5
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/play", nil)
		r.Header.Set("Range", "bytes="+strconv.Itoa(start)+"-"+strconv.Itoa(stop))
		http.ServeContent(w, r, "archive.mp4", time.Time{}, view)
		if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), out[start:stop+1]) {
			t.Fatalf("range=%d %x", w.Code, w.Body.Bytes())
		}
		var part [6]byte
		if _, err := view.ReadAt(part[:], int64(start)); err != nil || !bytes.Equal(part[:], out[start:start+6]) {
			t.Fatal("ReaderAt splice", err)
		}
	}
}
func TestNormalizedMP4LegacyAndInvalidClock(t *testing.T) {
	for _, data := range [][]byte{[]byte("legacy file"), normBox("ftyp", []byte("isom0000")), normFixture(0)} {
		v, err := NormalizedMP4(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(v)
		if !bytes.Equal(data, out) {
			t.Fatal("legacy clock changed")
		}
	}
	data := normFixture(1788912000)
	at := bytes.Index(data, []byte("tfdt"))
	data[at+4] = 2
	if _, err := NormalizedMP4(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("unsupported clock version accepted")
	}
}
func TestNormalizedMP4FFprobeRelativeDuration(t *testing.T) {
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " unavailable")
		}
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.mp4")
	raw := filepath.Join(dir, "epoch.mp4")
	normalized := filepath.Join(dir, "relative.mp4")
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg %v %s", err, out)
		}
	}
	run("-f", "lavfi", "-i", "testsrc2=size=64x64:rate=10", "-f", "lavfi", "-i", "sine=sample_rate=48000", "-t", "2", "-c:v", "libx264", "-g", "10", "-bf", "2", "-c:a", "aac", source)
	run("-copyts", "-itsoffset", "1788912000", "-i", source, "-map", "0", "-c", "copy", "-avoid_negative_ts", "disabled", "-movflags", "+frag_keyframe+empty_moov+default_base_moof+frag_discont", "-write_prft", "pts", "-use_editlist", "0", raw)
	f, err := os.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, _ := f.Stat()
	view, err := NormalizedMP4(f, stat.Size())
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(normalized)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(out, view)
	out.Close()
	if err != nil {
		t.Fatal(err)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration:stream=start_time", "-of", "json", normalized).CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe %v %s", err, probe)
	}
	var result struct {
		Format  struct{ Duration string }
		Streams []struct {
			StartTime string `json:"start_time"`
		}
	}
	if err := json.Unmarshal(probe, &result); err != nil {
		t.Fatal(err)
	}
	duration, _ := strconv.ParseFloat(result.Format.Duration, 64)
	if duration < 1.9 || duration > 2.5 {
		t.Fatalf("unusable normalized duration %s", probe)
	}
	for _, s := range result.Streams {
		start, _ := strconv.ParseFloat(s.StartTime, 64)
		if start < 0 || start > 0.3 {
			t.Fatalf("nonrelative start %s", probe)
		}
	}
	run("-i", normalized, "-f", "null", "-")
}

func TestNormalizedMP4SingleMediaExtentAndReferences(t *testing.T) {
	original := normFixture(1788912000)
	original = append(original, normBox("prft", []byte{1, 0, 0, 0}, normU32(1), normU64(0xed00000112345678), normU64(1788912000*90000+181920+90000))...)
	view, err := NormalizedMP4(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(view)
	if err != nil {
		t.Fatal(err)
	}
	var sourceTail int64
	var ntps [][]byte
	for pos := int64(0); pos < int64(len(original)); {
		b, err := readBox(bytes.NewReader(original), pos, int64(len(original)))
		if err != nil {
			t.Fatal(err)
		}
		if b.kind == "moov" {
			sourceTail = pos + b.size
		}
		if b.kind == "prft" {
			ntps = append(ntps, original[pos+b.header+8:pos+b.header+16])
		}
		pos += b.size
	}
	var references, media int
	for pos := int64(0); pos < int64(len(out)); {
		b, err := readBox(bytes.NewReader(out), pos, int64(len(out)))
		if err != nil {
			t.Fatal(err)
		}
		switch b.kind {
		case "ftyp", "moov":
		case "prft":
			if references >= len(ntps) || !bytes.Equal(out[pos+b.header+8:pos+b.header+16], ntps[references]) {
				t.Fatal("producer NTP changed")
			}
			if got := binary.BigEndian.Uint64(out[pos+b.header+16:]); got != uint64(references)*90000 {
				t.Fatalf("reference media time %d", got)
			}
			references++
		case "mdat":
			media++
			if references != len(ntps) {
				t.Fatal("references not all before media")
			}
			if pos+b.size != int64(len(out)) {
				t.Fatal("browser would need to scan beyond media")
			}
			if !bytes.Equal(out[pos+b.header:], original[sourceTail:]) {
				t.Fatal("source tail changed")
			}
		default:
			t.Fatalf("unexpected top-level box %s", b.kind)
		}
		pos += b.size
	}
	if references != 2 || media != 1 {
		t.Fatalf("references=%d media extents=%d", references, media)
	}
}
