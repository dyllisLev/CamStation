package recordingmedia

import (
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"sort"
)

// NormalizedMP4 supplies a virtual finalized-file view. Only clock metadata is
// patched; canonical archive bytes, sample payloads and all byte offsets stay
// unchanged. SectionReader supplies Seek and HTTP Range support over ReaderAt.
// Legacy nonfragmented and relative-clock files pass through byte-for-byte.
func NormalizedMP4(r io.ReaderAt, size int64) (*io.SectionReader, error) {
	plain := io.NewSectionReader(r, 0, size)
	var signature [8]byte
	if size < 8 {
		return plain, nil
	}
	if _, err := r.ReadAt(signature[:], 0); err != nil {
		return nil, err
	}
	if string(signature[4:]) != "ftyp" {
		return plain, nil
	}
	tracks := map[uint32]track{}
	var fields []clockField
	var origin *big.Rat
	haveReference, haveFragment := false, false
	for pos := int64(0); pos < size; {
		b, err := readBox(r, pos, size)
		if err != nil {
			return nil, err
		}
		switch b.kind {
		case "moov":
			data, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			tracks, err = readTracks(data)
			if errors.Is(err, ErrNotFragmented) {
				return plain, nil
			}
			if err != nil {
				return nil, err
			}
		case "moof":
			data, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			first := !haveFragment
			err = walkClockBoxes(data, b.offset+b.header, func(child box, p []byte) error {
				if child.kind != "traf" {
					return nil
				}
				var id uint32
				if err := walkClockBoxes(p, child.offset+child.header, func(c box, v []byte) error {
					if c.kind == "tfhd" {
						if len(v) < 8 {
							return io.ErrUnexpectedEOF
						}
						id = u32(v, 4)
					}
					return nil
				}); err != nil {
					return err
				}
				t, ok := tracks[id]
				if !ok {
					return errors.New("normalization: unknown fragment track")
				}
				found := false
				err := walkClockBoxes(p, child.offset+child.header, func(c box, v []byte) error {
					if c.kind != "tfdt" {
						return nil
					}
					if found {
						return errors.New("normalization: duplicate decode time")
					}
					found = true
					f, err := readClockField(v, 4, c.offset+c.header, t.scale)
					if err != nil {
						return err
					}
					fields = append(fields, f)
					if first {
						at := new(big.Rat).SetFrac(new(big.Int).SetUint64(f.value), new(big.Int).SetUint64(uint64(t.scale)))
						if origin == nil || at.Cmp(origin) < 0 {
							origin = at
						}
					}
					return nil
				})
				if err != nil {
					return err
				}
				if !found {
					return errors.New("normalization: missing decode time")
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			haveFragment = true
			// Only the recorder's epoch clock needs normalization. Old relative fMP4
			// (including files whose PRFT NTP is absolute) keeps its established clock.
			if first && (origin == nil || origin.Cmp(big.NewRat(946684800, 1)) < 0) {
				return plain, nil
			}
		case "prft":
			p, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			if len(p) < 8 {
				return nil, io.ErrUnexpectedEOF
			}
			t, ok := tracks[u32(p, 4)]
			if !ok {
				return nil, errors.New("normalization: unknown reference track")
			}
			f, err := readClockField(p, 16, b.offset+b.header, t.scale)
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
			haveReference = true
		case "sidx":
			p, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			if len(p) < 12 {
				return nil, io.ErrUnexpectedEOF
			}
			f, err := readClockField(p, 12, b.offset+b.header, u32(p, 8))
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
		case "mfra":
			p, err := payload(r, b)
			if err != nil {
				return nil, err
			}
			err = walkClockBoxes(p, b.offset+b.header, func(c box, v []byte) error {
				if c.kind != "tfra" {
					return nil
				}
				if len(v) < 16 {
					return io.ErrUnexpectedEOF
				}
				t, ok := tracks[u32(v, 4)]
				if !ok {
					return errors.New("normalization: unknown random-access track")
				}
				width := 4
				if v[0] == 1 {
					width = 8
				} else if v[0] != 0 {
					return errors.New("normalization: unsupported tfra version")
				}
				lengths := u32(v, 8)
				stride := 2*width + int((lengths>>4)&3) + int((lengths>>2)&3) + int(lengths&3) + 3
				count := uint64(u32(v, 12))
				if count > uint64((len(v)-16)/stride) {
					return io.ErrUnexpectedEOF
				}
				for at, n := 16, uint64(0); n < count; at, n = at+stride, n+1 {
					f, err := readClockField(v, at, c.offset+c.header, t.scale)
					if err != nil {
						return err
					}
					fields = append(fields, f)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		pos += b.size
	}
	if !haveFragment {
		return plain, nil
	}
	if !haveReference || origin == nil {
		return nil, errors.New("normalization: epoch fragments lack producer reference")
	}
	patches := make([]clockPatch, 0, len(fields))
	for _, f := range fields {
		scaled := new(big.Rat).Mul(origin, new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(f.scale))))
		shift := new(big.Int).Quo(scaled.Num(), scaled.Denom())
		if !shift.IsUint64() || shift.Uint64() > f.value {
			return nil, errors.New("normalization: clock precedes common origin")
		}
		n := f.value - shift.Uint64()
		p := clockPatch{offset: f.offset, data: make([]byte, f.width)}
		if f.width == 8 {
			binary.BigEndian.PutUint64(p.data, n)
		} else {
			binary.BigEndian.PutUint32(p.data, uint32(n))
		}
		patches = append(patches, p)
	}
	sort.Slice(patches, func(i, j int) bool { return patches[i].offset < patches[j].offset })
	return io.NewSectionReader(&normalizedReader{source: r, patches: patches}, 0, size), nil
}

type clockField struct {
	offset int64
	value  uint64
	scale  uint32
	width  int
}
type clockPatch struct {
	offset int64
	data   []byte
}
type normalizedReader struct {
	source  io.ReaderAt
	patches []clockPatch
}

func (r *normalizedReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.source.ReadAt(p, off)
	i := sort.Search(len(r.patches), func(i int) bool { return r.patches[i].offset+int64(len(r.patches[i].data)) > off })
	for ; i < len(r.patches) && r.patches[i].offset < off+int64(n); i++ {
		patch := r.patches[i]
		start := max(off, patch.offset)
		end := min(off+int64(n), patch.offset+int64(len(patch.data)))
		copy(p[start-off:end-off], patch.data[start-patch.offset:end-patch.offset])
	}
	return n, err
}
func readClockField(p []byte, at int, base int64, scale uint32) (clockField, error) {
	if len(p) < 1 || scale == 0 {
		return clockField{}, errors.New("normalization: invalid clock")
	}
	width := 4
	if p[0] == 1 {
		width = 8
	} else if p[0] != 0 {
		return clockField{}, errors.New("normalization: unsupported clock version")
	}
	if at > len(p)-width {
		return clockField{}, io.ErrUnexpectedEOF
	}
	n := uint64(u32(p, at))
	if width == 8 {
		n = binary.BigEndian.Uint64(p[at : at+8])
	}
	return clockField{base + int64(at), n, scale, width}, nil
}
func walkClockBoxes(data []byte, base int64, fn func(box, []byte) error) error {
	for pos := int64(0); pos < int64(len(data)); {
		b, err := readBox(bytesReader(data), pos, int64(len(data)))
		if err != nil {
			return err
		}
		child := b
		child.offset += base
		if err := fn(child, data[pos+b.header:pos+b.size]); err != nil {
			return err
		}
		pos += b.size
	}
	return nil
}
