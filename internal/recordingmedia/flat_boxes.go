package recordingmedia

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

func flatBox(kind string, p []byte) []byte {
	b := make([]byte, 8+len(p))
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	copy(b[4:], kind)
	copy(b[8:], p)
	return b
}
func flat32(p []byte, n uint32) []byte { return binary.BigEndian.AppendUint32(p, n) }
func flat64(p []byte, n uint64) []byte { return binary.BigEndian.AppendUint64(p, n) }

func (m *flatMovie) movieBox(delta int64) ([]byte, error) {
	var out []byte
	err := children(m.metadata, func(kind string, p []byte) error {
		var b []byte
		var err error
		switch kind {
		case "mvhd":
			b, err = flatDurationBox(kind, p, m.duration, flatMovieScale)
		case "mvex":
			return nil
		case "trak":
			var id uint32
			err = children(p, func(k string, v []byte) error {
				if k != "tkhd" {
					return nil
				}
				at := 12
				if len(v) > 0 && v[0] == 1 {
					at = 20
				}
				if len(v) < at+4 {
					return io.ErrUnexpectedEOF
				}
				id = u32(v, at)
				return nil
			})
			if err != nil {
				return err
			}
			t := m.tracks[id]
			if t == nil {
				return errors.New("normalization: movie track missing")
			}
			if len(t.samples) == 0 {
				return nil
			}
			b, err = t.trackBox(p, delta)
		default:
			b = flatBox(kind, p)
		}
		if err != nil {
			return err
		}
		out = append(out, b...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) > maxMetadataBox {
		return nil, errors.New("normalization: sample metadata too large")
	}
	return flatBox("moov", out), nil
}
func (t *flatTrack) trackBox(p []byte, delta int64) ([]byte, error) {
	var out []byte
	err := children(p, func(kind string, v []byte) error {
		var b []byte
		var err error
		switch kind {
		case "tkhd":
			b, err = flatDurationBox(kind, v, t.delay+t.duration, 0)
			if err == nil {
				b = append(b, t.editBox()...)
			}
		case "edts":
			return errors.New("normalization: preexisting fragment edit list is unsupported")
		case "mdia":
			b, err = t.mediaBox(kind, v, delta)
		default:
			b = flatBox(kind, v)
		}
		if err != nil {
			return err
		}
		out = append(out, b...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return flatBox("trak", out), nil
}
func (t *flatTrack) mediaBox(container string, p []byte, delta int64) ([]byte, error) {
	var out []byte
	err := children(p, func(kind string, v []byte) error {
		var b []byte
		var err error
		switch kind {
		case "mdhd":
			b, err = flatDurationBox(kind, v, uint64(t.nextDTS-t.firstDTS), 0)
		case "minf":
			b, err = t.mediaBox(kind, v, delta)
		case "stbl":
			b, err = t.sampleTable(v, delta)
		default:
			b = flatBox(kind, v)
		}
		if err != nil {
			return err
		}
		out = append(out, b...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return flatBox(container, out), nil
}

// Version 1 makes long/high-timescale movie durations explicit without changing
// creation/modification times, track identity, language, matrix or codec data.
func flatDurationBox(kind string, p []byte, duration uint64, scale uint32) ([]byte, error) {
	if len(p) < 4 || p[0] > 1 {
		return nil, errors.New("normalization: unsupported duration version")
	}
	width := 4
	if p[0] == 1 {
		width = 8
	}
	middle := 4
	if kind == "tkhd" {
		middle = 8
	}
	durationAt := 4 + 2*width + middle
	if len(p) < durationAt+width {
		return nil, io.ErrUnexpectedEOF
	}
	out := append([]byte(nil), p[:4]...)
	out[0] = 1
	for at := 4; at < 4+2*width; at += width {
		n := uint64(u32(p, at))
		if width == 8 {
			n = binary.BigEndian.Uint64(p[at : at+8])
		}
		out = flat64(out, n)
	}
	out = append(out, p[4+2*width:durationAt]...)
	if scale != 0 {
		binary.BigEndian.PutUint32(out[20:24], scale)
	}
	out = flat64(out, duration)
	out = append(out, p[durationAt+width:]...)
	return flatBox(kind, out), nil
}
func (t *flatTrack) editBox() []byte {
	p := []byte{1, 0, 0, 0}
	count := uint32(1)
	if t.delay > 0 {
		count++
	}
	p = flat32(p, count)
	if t.delay > 0 {
		p = flat64(p, t.delay)
		p = flat64(p, math.MaxUint64)
		p = flat32(p, 0x10000)
	}
	p = flat64(p, t.duration)
	p = flat64(p, uint64(t.minPTS-t.firstDTS+t.ctsShift))
	p = flat32(p, 0x10000)
	return flatBox("edts", flatBox("elst", p))
}
func (t *flatTrack) sampleTable(old []byte, delta int64) ([]byte, error) {
	var out []byte
	haveDescription := false
	err := children(old, func(kind string, p []byte) error {
		switch kind {
		case "stsd":
			if len(p) < 8 || u32(p, 4) != 1 {
				return errors.New("normalization: unsupported sample descriptions")
			}
			haveDescription = true
			out = append(out, flatBox(kind, p)...)
		case "stts", "ctts", "stsc", "stco", "co64", "stss":
			if len(p) < 8 || u32(p, 4) != 0 {
				return errors.New("normalization: initial movie already contains samples")
			}
		case "stsz":
			if len(p) < 12 || u32(p, 8) != 0 {
				return errors.New("normalization: initial movie already contains sample sizes")
			}
		case "free":
		default:
			return errors.New("normalization: unsupported initial sample metadata")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !haveDescription {
		return nil, errors.New("normalization: missing sample description")
	}
	type entry struct{ count, value uint32 }
	var times, compositions []entry
	var sync []uint32
	negativeCTS, haveCTS := false, false
	for _, s := range t.samples {
		c := s.cts + t.ctsShift
		negativeCTS = negativeCTS || c < 0
		haveCTS = haveCTS || c != 0
	}
	appendEntry := func(entries []entry, n uint32) []entry {
		if len(entries) > 0 && entries[len(entries)-1].value == n {
			entries[len(entries)-1].count++
			return entries
		}
		return append(entries, entry{1, n})
	}
	sizes := make([]byte, 8)
	sizes = flat32(sizes, uint32(len(t.samples)))
	for i, s := range t.samples {
		times = appendEntry(times, s.duration)
		sizes = flat32(sizes, s.size)
		c := s.cts + t.ctsShift
		if negativeCTS && (c < math.MinInt32 || c > math.MaxInt32) || !negativeCTS && (c < 0 || c > math.MaxUint32) {
			return nil, errors.New("normalization: composition time overflows")
		}
		compositions = appendEntry(compositions, uint32(c))
		if s.flags&0x10000 == 0 && (s.flags>>24)&3 != 1 {
			sync = append(sync, uint32(i+1))
		}
	}
	table := func(kind string, entries []entry, version byte) []byte {
		p := []byte{version, 0, 0, 0}
		p = flat32(p, uint32(len(entries)))
		for _, e := range entries {
			p = flat32(p, e.count)
			p = flat32(p, e.value)
		}
		return flatBox(kind, p)
	}
	out = append(out, table("stts", times, 0)...)
	if haveCTS {
		version := byte(0)
		if negativeCTS {
			version = 1
		}
		out = append(out, table("ctts", compositions, version)...)
	}
	if t.info.video {
		p := make([]byte, 4)
		p = flat32(p, uint32(len(sync)))
		for _, n := range sync {
			p = flat32(p, n)
		}
		out = append(out, flatBox("stss", p)...)
	}
	out = append(out, flatBox("stsz", sizes)...)
	chunks := make([]byte, 8)
	offsets := make([]byte, 4)
	offsets = flat32(offsets, uint32(len(t.chunks)))
	var entries uint32
	var previous uint32
	for i, c := range t.chunks {
		if i == 0 || c.samples != previous {
			chunks = flat32(chunks, uint32(i+1))
			chunks = flat32(chunks, c.samples)
			chunks = flat32(chunks, 1)
			entries++
			previous = c.samples
		}
		if delta > 0 && c.offset > math.MaxInt64-delta || delta < 0 && c.offset < -delta {
			return nil, errors.New("normalization: chunk offset overflows")
		}
		offset := c.offset + delta
		if offset < 0 {
			return nil, errors.New("normalization: negative chunk offset")
		}
		offsets = flat64(offsets, uint64(offset))
	}
	binary.BigEndian.PutUint32(chunks[4:8], entries)
	out = append(out, flatBox("stsc", chunks)...)
	out = append(out, flatBox("co64", offsets)...)
	return flatBox("stbl", out), nil
}
