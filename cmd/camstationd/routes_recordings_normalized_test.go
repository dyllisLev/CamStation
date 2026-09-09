package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRecordingDirectMediaNormalizesClockAndPreservesRange(t *testing.T) {
	box := func(kind string, p []byte) []byte {
		b := make([]byte, 8+len(p))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:], kind)
		copy(b[8:], p)
		return b
	}
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	tk := make([]byte, 16)
	binary.BigEndian.PutUint32(tk[12:], 1)
	md := make([]byte, 16)
	binary.BigEndian.PutUint32(md[12:], 90000)
	moov := box("moov", join(box("trak", join(box("tkhd", tk), box("mdia", box("mdhd", md)))), box("mvex", box("trex", make([]byte, 24)))))
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[4:], 1)
	dt := make([]byte, 12)
	dt[0] = 1
	binary.BigEndian.PutUint64(dt[4:], 1788912000*90000)
	prft := make([]byte, 24)
	prft[0] = 1
	binary.BigEndian.PutUint32(prft[4:], 1)
	binary.BigEndian.PutUint64(prft[8:], 0xed00000012345678)
	binary.BigEndian.PutUint64(prft[16:], 1788912000*90000)
	raw := join(box("ftyp", []byte("isom0000")), moov, box("prft", prft), box("moof", box("traf", join(box("tfhd", header), box("tfdt", dt)))), box("mdat", []byte("sample bytes")))
	expected := bytes.Clone(raw)
	dtAt := bytes.Index(raw, []byte("tfdt")) + 8
	prftAt := bytes.Index(raw, []byte("prft")) + 20
	clear(expected[dtAt : dtAt+8])
	clear(expected[prftAt : prftAt+8])
	server := newRecordingRouteServer(t)
	segment := server.createReadySegment(t, "epoch.mp4", string(raw))
	for _, action := range []string{"play", "download"} {
		for _, partial := range []bool{false, true} {
			target := fmt.Sprintf("/api/recordings/segments/%d/%s", segment.ID, action)
			r := httptest.NewRequest("GET", target, nil)
			want := expected
			code := 200
			if partial {
				r.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", dtAt+2, dtAt+5))
				want = expected[dtAt+2 : dtAt+6]
				code = 206
			}
			w := httptest.NewRecorder()
			server.handler.ServeHTTP(w, r)
			if w.Code != code || !bytes.Equal(w.Body.Bytes(), want) {
				t.Fatalf("%s partial=%v: status %d body %x", action, partial, w.Code, w.Body.Bytes())
			}
			if w.Header().Get("Content-Type") != "video/mp4" {
				t.Fatal("missing media content type")
			}
			if action == "download" && w.Header().Get("Content-Disposition") == "" {
				t.Fatal("download lost filename")
			}
		}
	}
	after, err := os.ReadFile(segment.FinalPath)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("HTTP mutated canonical recording", err)
	}
	// Unsupported epoch timing fails closed rather than returning unseekable media.
	raw[dtAt-4] = 2
	if err := os.WriteFile(segment.FinalPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/recordings/segments/%d/play", segment.ID), nil)
	w := httptest.NewRecorder()
	server.handler.ServeHTTP(w, r)
	if w.Code != 500 || bytes.Contains(w.Body.Bytes(), []byte(segment.FinalPath)) {
		t.Fatalf("unsafe preparation error: %d %s", w.Code, w.Body.String())
	}
}
