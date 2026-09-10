package recordingmedia

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/big"
)

// flatMovie describes samples without owning their payload. The virtual file
// replaces moov, hides fragment indexes, and leaves every mdat byte in place.
type flatMovie struct {
	moov       box
	metadata   []byte
	tracks     map[uint32]*flatTrack
	patches    []clockPatch
	references []flatReference
	origin     *big.Rat
	duration   uint64
}
type flatTrack struct {
	info              track
	samples           []flatSample
	chunks            []flatChunk
	firstDTS, nextDTS int64
	minPTS, maxPTS    int64
	ctsShift          int64
	delay, duration   uint64 // movie timescale
}
type flatSample struct {
	duration, size, flags uint32
	cts                   int64
}
type flatChunk struct {
	offset  int64
	samples uint32
}
type flatReference struct {
	field clockField
	track uint32
}

const flatMovieScale uint32 = 1000000

func readFlatMovie(r io.ReaderAt, size int64) (*flatMovie, error) {
	m := &flatMovie{tracks: map[uint32]*flatTrack{}}
	defaults := map[uint32]track{}
	var pending []run
	var referenceBoxes []box
	var sequence int64
	haveFragment := false
	for pos := int64(0); pos < size; {
		b, err := readBox(r, pos, size)
		if err != nil {
			return nil, err
		}
		switch b.kind {
		case "moov":
			if m.moov.size != 0 {
				return nil, errors.New("normalization: multiple movie boxes")
			}
			data, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			defaults, err = readTracks(data)
			if errors.Is(err, ErrNotFragmented) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			m.moov, m.metadata = b, data
			for id, t := range defaults {
				m.tracks[id] = &flatTrack{info: t}
			}
		case "moof":
			if pending != nil {
				return nil, errors.New("normalization: fragment missing media data")
			}
			data, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			// Validate clock versions before deciding whether this is a legacy
			// relative file; an unknown version must not masquerade as a small tfdt.
			if err := children(data, func(kind string, p []byte) error {
				if kind != "traf" {
					return nil
				}
				return children(p, func(k string, v []byte) error {
					if k != "tfdt" && k != "trun" {
						return nil
					}
					if len(v) < 4 {
						return io.ErrUnexpectedEOF
					}
					if v[0] > 1 {
						return errors.New("normalization: unsupported fragment clock version")
					}
					return nil
				})
			}); err != nil {
				return nil, err
			}
			if !haveFragment {
				// Classification uses only track clocks. Legacy fragments may use
				// absolute data offsets or pre-epoch PRFT values that this epoch
				// recorder parser deliberately does not accept.
				var origin *big.Rat
				err := children(data, func(kind string, p []byte) error {
					if kind != "traf" {
						return nil
					}
					var id uint32
					var clock []byte
					if err := children(p, func(k string, v []byte) error {
						if k == "tfhd" && len(v) >= 8 {
							id = u32(v, 4)
						}
						if k == "tfdt" {
							clock = v
						}
						return nil
					}); err != nil {
						return err
					}
					t, ok := defaults[id]
					if !ok {
						return errors.New("normalization: unknown clock track")
					}
					f, err := readClockField(clock, 4, 0, t.scale)
					if err != nil {
						return err
					}
					at := new(big.Rat).SetFrac(new(big.Int).SetUint64(f.value), new(big.Int).SetUint64(uint64(t.scale)))
					if origin == nil || at.Cmp(origin) < 0 {
						origin = at
					}
					return nil
				})
				if err != nil {
					return nil, err
				}
				if origin == nil || origin.Cmp(big.NewRat(946684800, 1)) < 0 {
					return nil, nil
				}
			}
			runs, seq, err := readRuns(data, defaults, b.offset)
			if err != nil {
				return nil, err
			}
			if seq <= sequence {
				return nil, errors.New("normalization: fragment sequence regressed")
			}
			sequence = seq
			for _, rr := range runs {
				t := m.tracks[rr.track]
				if len(t.samples) > 0 && rr.first != t.nextDTS {
					return nil, errors.New("normalization: decode time discontinuity")
				}
				if len(t.samples) == 0 {
					t.firstDTS = rr.first
					t.minPTS = rr.start
					t.maxPTS = rr.end
				}
				t.minPTS = min(t.minPTS, rr.start)
				t.maxPTS = max(t.maxPTS, rr.end)
				t.nextDTS = rr.decodeEnd
			}
			if err := m.readSamples(data, b.offset); err != nil {
				return nil, err
			}
			haveFragment = true
			pending = runs
			m.patches = append(m.patches, clockPatch{b.offset + 4, []byte("free")})
		case "mdat":
			if m.moov.size == 0 {
				break
			} // Ordinary MP4 may put its moov after mdat.
			if pending == nil {
				return nil, errors.New("normalization: unindexed media data")
			}
			for _, rr := range pending {
				for _, span := range rr.ranges {
					if span.start < b.offset+b.header || span.end > b.offset+b.size {
						return nil, errors.New("normalization: sample outside media data")
					}
				}
			}
			pending = nil
		case "prft":
			referenceBoxes = append(referenceBoxes, b)
		case "mfra", "sidx":
			// Their original offsets/times index fragments. Flat sample tables below
			// replace them; leaving either active would re-enable fragment inference.
			m.patches = append(m.patches, clockPatch{b.offset + 4, []byte("free")})
		}
		pos += b.size
	}
	if !haveFragment {
		return nil, nil
	}
	if pending != nil {
		return nil, errors.New("normalization: incomplete finalized fragment")
	}
	for _, b := range referenceBoxes {
		p, err := payload(r, b)
		if err != nil {
			return nil, err
		}
		ref, err := readReference(p)
		if err != nil {
			return nil, err
		}
		t, ok := defaults[ref.track]
		if !ok {
			return nil, errors.New("normalization: unknown reference track")
		}
		f, err := readClockField(p, 16, b.offset+b.header, t.scale)
		if err != nil {
			return nil, err
		}
		m.references = append(m.references, flatReference{f, ref.track})
	}
	if len(m.references) == 0 {
		return nil, errors.New("normalization: epoch fragments lack producer reference")
	}
	// Normal MP4 starts its movie at the earliest presented sample. Decode
	// preroll stays in the sample tables; edit lists preserve the A/V relation
	// without introducing an empty interval before every track.
	m.origin = nil
	for _, t := range m.tracks {
		if len(t.samples) > 0 {
			at := new(big.Rat).SetFrac(big.NewInt(t.minPTS), new(big.Int).SetUint64(uint64(t.info.scale)))
			if m.origin == nil || at.Cmp(m.origin) < 0 {
				m.origin = at
			}
		}
	}
	for _, t := range m.tracks {
		if len(t.samples) == 0 {
			continue
		}
		if t.minPTS < t.firstDTS {
			t.ctsShift = t.firstDTS - t.minPTS
		}
		at := new(big.Rat).SetFrac(big.NewInt(t.minPTS), new(big.Int).SetUint64(uint64(t.info.scale)))
		t.delay = ceilMovieTime(new(big.Rat).Sub(at, m.origin))
		span := new(big.Rat).SetFrac(big.NewInt(t.maxPTS-t.minPTS), new(big.Int).SetUint64(uint64(t.info.scale)))
		t.duration = ceilMovieTime(span)
		if t.duration > math.MaxUint64-t.delay {
			return nil, errors.New("normalization: movie duration overflows")
		}
		m.duration = max(m.duration, t.delay+t.duration)
	}
	// Keep NTP bytes unchanged while expressing media_time in the new track's
	// media clock (the edit list separately maps that clock into movie time).
	for _, ref := range m.references {
		t := m.tracks[ref.track]
		if len(t.samples) == 0 {
			return nil, errors.New("normalization: reference without samples")
		}
		v := new(big.Int).SetUint64(ref.field.value)
		v.Sub(v, big.NewInt(t.firstDTS))
		v.Add(v, big.NewInt(t.ctsShift))
		if !v.IsUint64() || (ref.field.width == 4 && v.Uint64() > math.MaxUint32) {
			return nil, errors.New("normalization: reference clock overflows")
		}
		p := make([]byte, ref.field.width)
		if len(p) == 8 {
			binary.BigEndian.PutUint64(p, v.Uint64())
		} else {
			binary.BigEndian.PutUint32(p, uint32(v.Uint64()))
		}
		m.patches = append(m.patches, clockPatch{ref.field.offset, p})
	}
	return m, nil
}

func ceilMovieTime(v *big.Rat) uint64 {
	n := new(big.Int).Mul(v.Num(), new(big.Int).SetUint64(uint64(flatMovieScale)))
	n.Add(n, new(big.Int).Sub(v.Denom(), big.NewInt(1)))
	return n.Quo(n, v.Denom()).Uint64()
}

func (m *flatMovie) readSamples(data []byte, moofOffset int64) error {
	return children(data, func(kind string, p []byte) error {
		if kind == "mfhd" || kind == "free" {
			return nil
		}
		if kind != "traf" {
			return errors.New("normalization: unsupported fragment metadata")
		}
		var t *flatTrack
		var duration, size, flags uint32
		return children(p, func(kind string, v []byte) error {
			if len(v) < 4 {
				return io.ErrUnexpectedEOF
			}
			bits := u32(v, 0) & 0xffffff
			switch kind {
			case "tfhd":
				if len(v) < 8 {
					return io.ErrUnexpectedEOF
				}
				t = m.tracks[u32(v, 4)]
				if t == nil {
					return errors.New("normalization: unknown sample track")
				}
				duration, size, flags = t.info.duration, t.info.size, t.info.flags
				at := 8
				take := func() (uint32, error) {
					if len(v)-at < 4 {
						return 0, io.ErrUnexpectedEOF
					}
					n := u32(v, at)
					at += 4
					return n, nil
				}
				if bits&1 != 0 || bits&0x020000 == 0 {
					return errors.New("normalization: unsupported fragment base")
				}
				if bits&2 != 0 {
					n, err := take()
					if err != nil {
						return err
					}
					if n != 1 {
						return errors.New("normalization: unsupported sample description")
					}
				}
				var err error
				if bits&8 != 0 {
					duration, err = take()
					if err != nil {
						return err
					}
				}
				if bits&16 != 0 {
					size, err = take()
					if err != nil {
						return err
					}
				}
				if bits&32 != 0 {
					flags, err = take()
					if err != nil {
						return err
					}
				}
			case "trun":
				if t == nil || len(v) < 8 {
					return io.ErrUnexpectedEOF
				}
				count := u32(v, 4)
				if count == 0 || count > 1000000 {
					return errors.New("normalization: invalid sample count")
				}
				at := 8
				take := func() (uint32, error) {
					if len(v)-at < 4 {
						return 0, io.ErrUnexpectedEOF
					}
					n := u32(v, at)
					at += 4
					return n, nil
				}
				if bits&1 == 0 {
					return errors.New("normalization: sample run lacks offset")
				}
				raw, err := take()
				if err != nil {
					return err
				}
				offset := moofOffset + int64(int32(raw))
				firstFlags := flags
				if bits&4 != 0 {
					firstFlags, err = take()
					if err != nil {
						return err
					}
				}
				for i := uint32(0); i < count; i++ {
					s := flatSample{duration: duration, size: size, flags: flags}
					if i == 0 {
						s.flags = firstFlags
					}
					if bits&0x100 != 0 {
						s.duration, err = take()
						if err != nil {
							return err
						}
					}
					if bits&0x200 != 0 {
						s.size, err = take()
						if err != nil {
							return err
						}
					}
					if bits&0x400 != 0 {
						s.flags, err = take()
						if err != nil {
							return err
						}
					}
					if bits&0x800 != 0 {
						n, e := take()
						if e != nil {
							return e
						}
						s.cts = int64(n)
						if v[0] == 1 {
							s.cts = int64(int32(n))
						} else if v[0] != 0 {
							return errors.New("normalization: unsupported run version")
						}
					}
					t.samples = append(t.samples, s)
				}
				t.chunks = append(t.chunks, flatChunk{offset, count})
			case "tfdt", "free":
			default:
				return errors.New("normalization: unsupported track fragment metadata")
			}
			return nil
		})
	})
}
