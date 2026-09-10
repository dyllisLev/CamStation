package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func TestRecordingDirectMediaNormalizesClockAndPreservesRange(t *testing.T) {
	// A 0.6-second synthetic blue image + sine tone, H.264/AAC, remuxed with
	// copyts/itsoffset=1788912000 and the recorder's fragmented MP4 flags.
	// This includes real sample tables and payloads; it contains no camera data.
	raw, err := os.ReadFile("testdata/recording-epoch.mp4")
	if err != nil {
		t.Fatal(err)
	}
	server := newRecordingRouteServer(t)
	segment := server.createReadySegment(t, "epoch.mp4", string(raw))
	var full []byte
	for _, action := range []string{"play", "download"} {
		target := fmt.Sprintf("/api/recordings/segments/%d/%s", segment.ID, action)
		w := httptest.NewRecorder()
		server.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d body %s", action, w.Code, w.Body.String())
		}
		body := w.Body.Bytes()
		if bytes.Equal(body, raw) {
			t.Fatal("epoch recording was served without normalization")
		}
		if full == nil {
			full = bytes.Clone(body)
		} else if !bytes.Equal(body, full) {
			t.Fatal("play and download supplied different media")
		}
		if w.Header().Get("Content-Length") != strconv.Itoa(len(full)) {
			t.Fatal("response length does not match the prepared file")
		}
		if w.Header().Get("Content-Type") != "video/mp4" {
			t.Fatal("missing media content type")
		}
		if action == "download" && w.Header().Get("Content-Disposition") == "" {
			t.Fatal("download lost filename")
		}
		if !bytes.Equal(directMediaPayloads(t, full), directMediaPayloads(t, raw)) {
			t.Fatal("HTTP preparation changed encoded sample payloads")
		}
		for _, span := range [][2]int{{0, 31}, {len(full)/2 - 8, len(full)/2 + 8}, {len(full) - 16, len(full) - 1}} {
			r := httptest.NewRequest(http.MethodGet, target, nil)
			r.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", span[0], span[1]))
			w := httptest.NewRecorder()
			server.handler.ServeHTTP(w, r)
			if w.Code != http.StatusPartialContent || !bytes.Equal(w.Body.Bytes(), full[span[0]:span[1]+1]) {
				t.Fatalf("%s range %v: status %d, incorrect bytes", action, span, w.Code)
			}
			wantRange := fmt.Sprintf("bytes %d-%d/%d", span[0], span[1], len(full))
			if w.Header().Get("Content-Range") != wantRange {
				t.Fatalf("Content-Range = %q, want %q", w.Header().Get("Content-Range"), wantRange)
			}
		}
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(full)))
		w = httptest.NewRecorder()
		server.handler.ServeHTTP(w, r)
		if w.Code != http.StatusRequestedRangeNotSatisfiable || w.Header().Get("Content-Range") != fmt.Sprintf("bytes */%d", len(full)) {
			t.Fatal("invalid range did not use the prepared file's length")
		}
	}
	after, err := os.ReadFile(segment.FinalPath)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("HTTP mutated canonical recording", err)
	}
	// Unsupported epoch timing must fail without exposing internal file paths.
	dt := bytes.Index(raw, []byte("tfdt"))
	if dt < 0 {
		t.Fatal("fixture lacks fragment decode time")
	}
	raw[dt+4] = 2
	if err := os.WriteFile(segment.FinalPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/recordings/segments/%d/play", segment.ID), nil)
	w := httptest.NewRecorder()
	server.handler.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError || bytes.Contains(w.Body.Bytes(), []byte(segment.FinalPath)) {
		t.Fatalf("unsafe preparation error: status=%d responseBytes=%d", w.Code, w.Body.Len())
	}
}

func directMediaPayloads(t *testing.T, file []byte) []byte {
	t.Helper()
	var result []byte
	for at := 0; at < len(file); {
		if len(file)-at < 8 {
			t.Fatal("truncated MP4 box")
		}
		size, header := uint64(binary.BigEndian.Uint32(file[at:])), 8
		if size == 1 {
			if len(file)-at < 16 {
				t.Fatal("truncated extended MP4 box")
			}
			size, header = binary.BigEndian.Uint64(file[at+8:]), 16
		} else if size == 0 {
			size = uint64(len(file) - at)
		}
		if size < uint64(header) || size > uint64(len(file)-at) {
			t.Fatal("invalid MP4 box size")
		}
		if string(file[at+4:at+8]) == "mdat" {
			result = append(result, file[at+header:at+int(size)]...)
		}
		at += int(size)
	}
	if len(result) == 0 {
		t.Fatal("MP4 contains no encoded payloads")
	}
	return result
}
