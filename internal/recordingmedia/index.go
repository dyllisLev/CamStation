// Package recordingmedia indexes the committed prefix of recorder fragmented MP4s.
// Absolute time is embedded by FFmpeg's write_prft=pts with epoch input PTS.
package recordingmedia

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
)

var ErrNotFragmented = errors.New("media has no fragmented recording metadata")

const TimeBasis = "server_received_prft"
const maxMetadataBox = 16 << 20

type Index struct {
	InitLength      int64
	CommittedOffset int64
	VideoCodec      string
	TimeBasis       string
	Fragments       []Fragment
}

type Fragment struct {
	Sequence     int64
	Offset       int64
	Length       int64
	MediaStartMs int64
	MediaEndMs   int64
	StartMs      int64
	EndMs        int64
}

type box struct {
	offset, size, header int64
	kind                 string
}
type track struct {
	id, scale, duration, size, flags uint32
	video                            bool
	codec                            string
}
type reference struct {
	track   uint32
	epochMs int64
}
type sampleRange struct{ start, end int64 }
type run struct {
	track                                  uint32
	first, firstPTS, start, end, decodeEnd int64
	key                                    bool
	ranges                                 []sampleRange
}

func Inspect(path string) (Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return Index{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Index{}, err
	}
	return ReadIndex(f, info.Size())
}

// ReadIndex never reads beyond size, and excludes a partial last box or fragment.
// It accepts the recorder's default-base-is-moof layout; arbitrary MP4 variants
// are served through the existing finalized-file path instead.
func ReadIndex(r io.ReaderAt, size int64) (Index, error) {
	var idx Index
	tracks := map[uint32]track{}
	var ref *reference
	var pending *box
	var pendingOffset int64
	var runs []run
	previousDecode := map[uint32]int64{}
	var previousSequence int64
	var mediaOriginMs int64
	for pos := int64(0); pos < size; {
		b, err := readBox(r, pos, size)
		if errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return idx, err
		}
		switch b.kind {
		case "moov":
			data, err := payload(r, b)
			if err != nil {
				return idx, err
			}
			tracks, err = readTracks(data)
			if err != nil {
				return idx, err
			}
			for _, t := range tracks {
				if t.video {
					idx.VideoCodec = t.codec
				}
			}
			idx.InitLength = pos + b.size
		case "prft":
			if pending != nil {
				return idx, errors.New("reference inside incomplete fragment")
			}
			data, err := payload(r, b)
			if err != nil {
				return idx, err
			}
			ref, err = readReference(data)
			if err != nil {
				return idx, err
			}
			pendingOffset = pos
		case "moof":
			if pending != nil {
				return idx, errors.New("fragment missing media data")
			}
			if idx.InitLength == 0 || len(tracks) == 0 || ref == nil {
				return idx, ErrNotFragmented
			}
			if ref.track == 0 || !tracks[ref.track].video {
				return idx, errors.New("reference does not identify video")
			}
			data, err := payload(r, b)
			if err != nil {
				return idx, err
			}
			var seq int64
			runs, seq, err = readRuns(data, tracks, pos)
			if err != nil {
				return idx, err
			}
			if seq <= previousSequence {
				return idx, errors.New("fragment sequence regressed")
			}
			previousSequence = seq
			pending = &b
		case "mdat":
			if pending == nil {
				return idx, ErrNotFragmented
			}
			var video *run
			for i := range runs {
				rr := &runs[i]
				for _, span := range rr.ranges {
					if span.start < pos+b.header || span.end > pos+b.size {
						return idx, errors.New("sample exceeds complete media data")
					}
				}
				if prev, ok := previousDecode[rr.track]; ok && rr.first != prev {
					return idx, errors.New("decode time discontinuity")
				}
				if rr.track == ref.track {
					video = rr
				}
			}
			if video == nil || !video.key {
				return idx, errors.New("fragment does not start with a video keyframe")
			}
			// hls.js places the first fragment at the earliest track decode
			// time. Preserve A/V offsets, including audio before the first video
			// keyframe, and expose that same normalized clock to the API.
			if len(idx.Fragments) == 0 {
				mediaOriginMs = math.MaxInt64
				for _, rr := range runs {
					mediaOriginMs = min(mediaOriginMs, ticksMs(rr.first, tracks[rr.track].scale))
				}
			}
			scale := tracks[video.track].scale
			start, end := ticksMs(video.start, scale), ticksMs(video.end, scale)
			// PRFT from this muxer identifies the first sample's PTS. The tfdt
			// either retains the common input clock or is local in older files.
			// B frames may present before the first sample.
			anchor := ticksMs(video.firstPTS, scale)
			frag := Fragment{Sequence: previousSequence, Offset: pendingOffset, Length: pos + b.size - pendingOffset,
				MediaStartMs: start - mediaOriginMs, MediaEndMs: end - mediaOriginMs, StartMs: ref.epochMs + start - anchor, EndMs: ref.epochMs + end - anchor}
			if frag.EndMs <= frag.StartMs {
				return idx, errors.New("empty presentation interval")
			}
			if len(idx.Fragments) > 0 {
				first := idx.Fragments[0]
				difference := (frag.StartMs - frag.MediaStartMs) - (first.StartMs - first.MediaStartMs)
				if difference < -2 || difference > 2 {
					return idx, errors.New("producer time discontinuity")
				}
			}
			idx.Fragments = append(idx.Fragments, frag)
			idx.CommittedOffset = pos + b.size
			idx.TimeBasis = TimeBasis
			for _, rr := range runs {
				previousDecode[rr.track] = rr.decodeEnd
			}
			pending = nil
			ref = nil
			runs = nil
		}
		pos += b.size
	}
	if idx.InitLength > 0 && len(tracks) == 0 {
		return idx, ErrNotFragmented
	}
	return idx, nil
}

func readBox(r io.ReaderAt, offset, end int64) (box, error) {
	if offset < 0 || end-offset < 8 {
		return box{}, io.ErrUnexpectedEOF
	}
	var head [16]byte
	if _, err := r.ReadAt(head[:8], offset); err != nil {
		return box{}, err
	}
	size := int64(binary.BigEndian.Uint32(head[:4]))
	header := int64(8)
	if size == 1 {
		if end-offset < 16 {
			return box{}, io.ErrUnexpectedEOF
		}
		if _, err := r.ReadAt(head[8:], offset+8); err != nil {
			return box{}, err
		}
		wide := binary.BigEndian.Uint64(head[8:])
		if wide > math.MaxInt64 {
			return box{}, errors.New("box size overflows")
		}
		size = int64(wide)
		header = 16
	}
	// A box extending to EOF is not a committed boundary in a growing file.
	if size == 0 {
		return box{}, io.ErrUnexpectedEOF
	}
	if size < header {
		return box{}, errors.New("invalid box size")
	}
	if size > end-offset {
		return box{}, io.ErrUnexpectedEOF
	}
	return box{offset: offset, size: size, header: header, kind: string(head[4:8])}, nil
}
func payload(r io.ReaderAt, b box) ([]byte, error) {
	if b.size-b.header > maxMetadataBox {
		return nil, errors.New("metadata box too large")
	}
	p := make([]byte, b.size-b.header)
	_, err := r.ReadAt(p, b.offset+b.header)
	return p, err
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func children(data []byte, fn func(string, []byte) error) error {
	for pos := int64(0); pos < int64(len(data)); {
		b, err := readBox(bytesReader(data), pos, int64(len(data)))
		if err != nil {
			return err
		}
		if err = fn(b.kind, data[pos+b.header:pos+b.size]); err != nil {
			return err
		}
		pos += b.size
	}
	return nil
}
func u32(b []byte, p int) uint32 { return binary.BigEndian.Uint32(b[p : p+4]) }

func readTracks(data []byte) (map[uint32]track, error) {
	tracks := map[uint32]track{}
	defaults := map[uint32]track{}
	err := children(data, func(kind string, p []byte) error {
		switch kind {
		case "trak":
			var t track
			err := children(p, func(k string, v []byte) error {
				switch k {
				case "tkhd":
					at := 12
					if len(v) > 0 && v[0] == 1 {
						at = 20
					}
					if len(v) < at+4 {
						return io.ErrUnexpectedEOF
					}
					t.id = u32(v, at)
				case "mdia":
					return children(v, func(k string, v []byte) error {
						switch k {
						case "mdhd":
							at := 12
							if len(v) > 0 && v[0] == 1 {
								at = 20
							}
							if len(v) < at+4 {
								return io.ErrUnexpectedEOF
							}
							t.scale = u32(v, at)
						case "hdlr":
							if len(v) < 12 {
								return io.ErrUnexpectedEOF
							}
							t.video = string(v[8:12]) == "vide"
						case "minf":
							return children(v, func(k string, v []byte) error {
								if k != "stbl" {
									return nil
								}
								return children(v, func(k string, v []byte) error {
									if k != "stsd" {
										return nil
									}
									if len(v) < 16 {
										return io.ErrUnexpectedEOF
									}
									t.codec = string(v[12:16])
									return nil
								})
							})
						}
						return nil
					})
				}
				return nil
			})
			if err != nil {
				return err
			}
			if t.id == 0 || t.scale == 0 {
				return errors.New("invalid track identity or timescale")
			}
			tracks[t.id] = t
		case "mvex":
			return children(p, func(k string, v []byte) error {
				if k != "trex" {
					return nil
				}
				if len(v) < 24 {
					return io.ErrUnexpectedEOF
				}
				defaults[u32(v, 4)] = track{duration: u32(v, 12), size: u32(v, 16), flags: u32(v, 20)}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(defaults) == 0 {
		return nil, ErrNotFragmented
	}
	for id, t := range tracks {
		d := defaults[id]
		t.duration = d.duration
		t.size = d.size
		t.flags = d.flags
		tracks[id] = t
	}
	return tracks, nil
}
func readReference(p []byte) (*reference, error) {
	if len(p) < 24 || p[0] != 1 {
		return nil, errors.New("invalid producer reference time")
	}
	ntp := binary.BigEndian.Uint64(p[8:16])
	seconds := int64(ntp>>32) - 2208988800
	// This is recorder epoch PTS, not an arbitrary MP4 wallclock assertion.
	if seconds < 946684800 || seconds > 4102444800 {
		return nil, ErrNotFragmented
	}
	ms := seconds*1000 + int64(math.Round(float64(uint32(ntp))*1000/(1<<32)))
	return &reference{track: u32(p, 4), epochMs: ms}, nil
}
func ticksMs(n int64, scale uint32) int64 {
	return int64(math.Round(float64(n) * 1000 / float64(scale)))
}

func readRuns(data []byte, tracks map[uint32]track, moofOffset int64) ([]run, int64, error) {
	var result []run
	var sequence int64
	err := children(data, func(kind string, p []byte) error {
		if kind == "mfhd" {
			if len(p) < 8 {
				return io.ErrUnexpectedEOF
			}
			sequence = int64(u32(p, 4))
			return nil
		}
		if kind != "traf" {
			return nil
		}
		var t track
		var rr run
		var decode int64
		var haveHeader, haveDecode, haveSample bool
		err := children(p, func(k string, v []byte) error {
			if len(v) < 4 {
				return io.ErrUnexpectedEOF
			}
			flags := u32(v, 0) & 0xffffff
			switch k {
			case "tfhd":
				if len(v) < 8 {
					return io.ErrUnexpectedEOF
				}
				var ok bool
				t, ok = tracks[u32(v, 4)]
				if !ok {
					return errors.New("unknown fragment track")
				}
				// The recording command explicitly uses default_base_moof.
				if flags&0x020000 == 0 || flags&1 != 0 {
					return errors.New("unsupported fragment data base")
				}
				at := 8
				next := func() (uint32, error) {
					if len(v)-at < 4 {
						return 0, io.ErrUnexpectedEOF
					}
					n := u32(v, at)
					at += 4
					return n, nil
				}
				var err error
				if flags&2 != 0 {
					if _, err = next(); err != nil {
						return err
					}
				}
				if flags&8 != 0 {
					if t.duration, err = next(); err != nil {
						return err
					}
				}
				if flags&16 != 0 {
					if t.size, err = next(); err != nil {
						return err
					}
				}
				if flags&32 != 0 {
					if t.flags, err = next(); err != nil {
						return err
					}
				}
				rr.track = t.id
				haveHeader = true
			case "tfdt":
				if len(v) < 8 {
					return io.ErrUnexpectedEOF
				}
				n := uint64(u32(v, 4))
				if v[0] == 1 {
					if len(v) < 12 {
						return io.ErrUnexpectedEOF
					}
					n = binary.BigEndian.Uint64(v[4:12])
				}
				if n > math.MaxInt64 {
					return errors.New("decode time overflows")
				}
				decode = int64(n)
				rr.first = decode
				haveDecode = true
			case "trun":
				if !haveHeader || !haveDecode || len(v) < 8 {
					return errors.New("sample run missing track header")
				}
				count := u32(v, 4)
				if count == 0 || count > 1000000 {
					return errors.New("invalid sample count")
				}
				at := 8
				next := func() (uint32, error) {
					if len(v)-at < 4 {
						return 0, io.ErrUnexpectedEOF
					}
					n := u32(v, at)
					at += 4
					return n, nil
				}
				if flags&1 == 0 {
					return errors.New("sample run missing data offset")
				}
				raw, err := next()
				if err != nil {
					return err
				}
				delta := int64(int32(raw))
				if delta < 0 || delta > math.MaxInt64-moofOffset {
					return errors.New("invalid sample data offset")
				}
				dataStart := moofOffset + delta
				dataEnd := dataStart
				firstFlags := t.flags
				if flags&4 != 0 {
					firstFlags, err = next()
					if err != nil {
						return err
					}
				}
				for i := uint32(0); i < count; i++ {
					duration, size, sampleFlags := t.duration, t.size, t.flags
					if i == 0 {
						sampleFlags = firstFlags
					}
					if flags&0x100 != 0 {
						if duration, err = next(); err != nil {
							return err
						}
					}
					if flags&0x200 != 0 {
						if size, err = next(); err != nil {
							return err
						}
					}
					if flags&0x400 != 0 {
						if sampleFlags, err = next(); err != nil {
							return err
						}
					}
					var cts int64
					if flags&0x800 != 0 {
						raw, err = next()
						if err != nil {
							return err
						}
						cts = int64(raw)
						if v[0] == 1 {
							cts = int64(int32(raw))
						}
					}
					// AAC packets can share a DTS at a fragment boundary. A zero
					// duration then describes their timing, not an empty payload.
					if size == 0 || (duration == 0 && (t.video || t.codec != "mp4a")) {
						return errors.New("empty sample")
					}
					if decode > math.MaxInt64-int64(duration) || dataEnd > math.MaxInt64-int64(size) || cts > math.MaxInt64-decode {
						return errors.New("sample arithmetic overflows")
					}
					pts := decode + cts
					if pts < 0 || pts > math.MaxInt64-int64(duration) {
						return errors.New("invalid presentation time")
					}
					if !haveSample {
						rr.firstPTS = pts
						rr.start = pts
						rr.end = pts + int64(duration)
						rr.key = sampleFlags&0x10000 == 0 && (sampleFlags>>24)&3 != 1
						haveSample = true
					}
					if pts < rr.start {
						rr.start = pts
					}
					if pts+int64(duration) > rr.end {
						rr.end = pts + int64(duration)
					}
					decode += int64(duration)
					dataEnd += int64(size)
				}
				rr.ranges = append(rr.ranges, sampleRange{start: dataStart, end: dataEnd})
				rr.decodeEnd = decode
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !haveSample {
			return errors.New("fragment track has no samples")
		}
		result = append(result, rr)
		return nil
	})
	return result, sequence, err
}
