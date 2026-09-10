package recordingmedia

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// NormalizedMP4 presents epoch-clock recorder fragments as a normal finalized
// MP4. It synthesizes sample tables and edit lists, reads payload from the
// original archive, and never writes or transcodes that archive. The explicit
// movie duration avoids fragmented-MP4 duration inference counting an initial
// A/V delay twice. Legacy MP4 and relative-clock fMP4 pass through unchanged.
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
	movie, err := readFlatMovie(r, size)
	if err != nil {
		return nil, err
	}
	if movie == nil {
		return plain, nil
	}
	moov, err := movie.movieBox(0)
	if err != nil {
		return nil, err
	}
	delta := int64(len(moov)) + int64(len(movie.referenceData)) + 16 - movie.moov.size
	if delta > 0 && size > math.MaxInt64-delta {
		return nil, errors.New("normalization: file size overflows")
	}
	moov, err = movie.movieBox(delta)
	if err != nil {
		return nil, err
	}
	// Chrome otherwise seeks across every interspersed fragment box before
	// loadedmetadata. Put every producer reference beside moov, then expose
	// the untouched source tail as one mdat ending at EOF. Old box headers
	// are harmless padding between the samples addressed by co64.
	metadata := append(moov, movie.referenceData...)
	metadata = append(metadata, 0, 0, 0, 1, 'm', 'd', 'a', 't')
	metadata = binary.BigEndian.AppendUint64(metadata, uint64(size-movie.moov.offset-movie.moov.size)+16)
	view := &movieReader{source: r, start: movie.moov.offset, removed: movie.moov.size, metadata: metadata, size: size + delta}
	return io.NewSectionReader(view, 0, view.size), nil
}

// Only moov, producer references and the mdat header are materialized. All
// prefix/tail reads still address the
// immutable source; partial reads may cross either replacement boundary.
type movieReader struct {
	source               io.ReaderAt
	start, removed, size int64
	metadata             []byte
}

func (r *movieReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative read offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}
	requested := len(p)
	n := 0
	for len(p) > 0 && off < r.size {
		var got int
		var err error
		switch {
		case off < r.start:
			count := min(int64(len(p)), r.start-off)
			got, err = r.source.ReadAt(p[:count], off)
		case off < r.start+int64(len(r.metadata)):
			got = copy(p, r.metadata[off-r.start:])
		default:
			count := min(int64(len(p)), r.size-off)
			got, err = r.source.ReadAt(p[:count], off-int64(len(r.metadata))+r.removed)
		}
		n += got
		off += int64(got)
		p = p[got:]
		if err != nil {
			return n, err
		}
		if got == 0 {
			return n, io.ErrNoProgress
		}
	}
	if n < requested {
		return n, io.EOF
	}
	return n, nil
}

type clockField struct {
	offset int64
	value  uint64
	scale  uint32
	width  int
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
