package stream

import (
	"bytes"
	"io"
	"sync"

	"camstation/internal/store"
)

const maxBufferedLogLine = 64 << 10

type redactingLineWriter struct {
	destination io.Writer

	mu       sync.Mutex
	pending  []byte
	dropping bool
}

func newRedactingLineWriter(destination io.Writer) *redactingLineWriter {
	return &redactingLineWriter{destination: destination}
}

func (w *redactingLineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := len(data)
	for len(data) > 0 {
		if w.dropping {
			newline := bytes.IndexByte(data, '\n')
			if newline < 0 {
				return written, nil
			}
			w.dropping = false
			data = data[newline+1:]
			continue
		}
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			if len(w.pending)+len(data) > maxBufferedLogLine {
				w.pending = nil
				w.dropping = true
				return written, nil
			}
			w.pending = append(w.pending, data...)
			return written, nil
		}
		w.pending = append(w.pending, data[:newline]...)
		line := store.RedactText(string(w.pending)) + "\n"
		w.pending = w.pending[:0]
		if _, err := io.WriteString(w.destination, line); err != nil {
			return written, err
		}
		data = data[newline+1:]
	}
	return written, nil
}
