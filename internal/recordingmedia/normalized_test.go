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
	tk := make([]byte, 16)
	binary.BigEndian.PutUint32(tk[12:], id)
	md := make([]byte, 16)
	binary.BigEndian.PutUint32(md[12:], scale)
	return normBox("trak", normBox("tkhd", tk), normBox("mdia", normBox("mdhd", md)))
}
func normFixture(epoch uint64) []byte {
	video := epoch*90000 + 181920
	audio := epoch * 48000
	moov := normBox("moov", normTrack(1, 90000), normTrack(2, 48000), normBox("mvex", normBox("trex", make([]byte, 24))))
	traf := func(id uint32, n uint64) []byte {
		return normBox("traf", normBox("tfhd", make([]byte, 4), normU32(id)), normBox("tfdt", []byte{1, 0, 0, 0}, normU64(n)))
	}
	prft := normBox("prft", []byte{1, 0, 0, 0}, normU32(1), normU64(0xed00000012345678), normU64(video+18000))
	tfra := normBox("tfra", []byte{1, 0, 0, 0}, normU32(1), normU32(0), normU32(1), normU64(video+18000), normU64(987), []byte{1, 1, 1})
	sidx := normBox("sidx", []byte{1, 0, 0, 0}, normU32(1), normU32(90000), normU64(video+18000), normU64(0), make([]byte, 4))
	return bytes.Join([][]byte{normBox("ftyp", []byte("isom0000")), moov, sidx, prft, normBox("moof", traf(1, video), traf(2, audio)), normBox("mdat", []byte("unaltered sample payload")), normBox("mfra", tfra)}, nil)
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
	if len(out) != len(original) || !bytes.Equal(original, saved) {
		t.Fatal("canonical bytes or length changed")
	}
	tfdt := bytes.Index(out, []byte("tfdt"))
	if got := binary.BigEndian.Uint64(out[tfdt+8:]); got != 181920 {
		t.Fatalf("A/V offset lost: %d", got)
	}
	second := bytes.Index(out[tfdt+4:], []byte("tfdt")) + tfdt + 4
	if got := binary.BigEndian.Uint64(out[second+8:]); got != 0 {
		t.Fatalf("earliest audio=%d", got)
	}
	for _, field := range []struct {
		kind string
		off  int
	}{{"prft", 20}, {"tfra", 20}, {"sidx", 16}} {
		at := bytes.Index(out, []byte(field.kind))
		if got := binary.BigEndian.Uint64(out[at+field.off:]); got != 199920 {
			t.Fatalf("%s=%d", field.kind, got)
		}
	}
	tfra := bytes.Index(out, []byte("tfra"))
	if binary.BigEndian.Uint64(out[tfra+28:]) != 987 {
		t.Fatal("random access byte offset changed")
	}
	prft := bytes.Index(out, []byte("prft"))
	if !bytes.Equal(out[prft+12:prft+20], original[prft+12:prft+20]) {
		t.Fatal("absolute NTP changed")
	}
	mdat := bytes.Index(out, []byte("mdat"))
	end := mdat + 4 + len("unaltered sample payload")
	if !bytes.Equal(out[mdat:end], original[mdat:end]) {
		t.Fatal("sample payload changed")
	}
	// Range starts/ends within an eight-byte patched field, rather than on boxes.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/play", nil)
	start := tfdt + 10
	stop := tfdt + 13
	r.Header.Set("Range", "bytes="+strconv.Itoa(start)+"-"+strconv.Itoa(stop))
	http.ServeContent(w, r, "archive.mp4", time.Time{}, view)
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), out[start:stop+1]) {
		t.Fatalf("range=%d %x", w.Code, w.Body.Bytes())
	}
	var part [3]byte
	if _, err := view.ReadAt(part[:], int64(tfdt+9)); err != nil || !bytes.Equal(part[:], out[tfdt+9:tfdt+12]) {
		t.Fatal("ReaderAt patch slice", err)
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
