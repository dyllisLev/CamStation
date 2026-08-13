package stream

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"camstation/internal/opslog"
	"camstation/internal/store"
)

const maxBufferedLogLine = 64 << 10

type redactingLineWriter struct {
	destination io.Writer
	logger      *opslog.Logger
	component   string
	fallback    opslog.Level

	mu       sync.Mutex
	pending  []byte
	dropping bool
}

func newRedactingLineWriter(destination io.Writer) *redactingLineWriter {
	return &redactingLineWriter{destination: destination}
}

func newOperationalLineWriter(logger *opslog.Logger, component string, fallback opslog.Level) *redactingLineWriter {
	return &redactingLineWriter{logger: logger, component: component, fallback: fallback}
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
				if w.logger != nil {
					if err := w.logger.Log(opslog.Warn, w.component, "line_dropped", opslog.Fields{
						ErrorCode: "line_too_long",
					}); err != nil {
						return written, err
					}
				}
				return written, nil
			}
			w.pending = append(w.pending, data...)
			return written, nil
		}
		w.pending = append(w.pending, data[:newline]...)
		line := store.RedactText(string(w.pending))
		w.pending = w.pending[:0]
		if w.logger != nil {
			level := go2RTCLineLevel(line, w.fallback)
			if err := w.logger.Log(level, w.component, "child_output", opslog.Fields{Message: line}); err != nil {
				return written, err
			}
		} else if w.destination != nil {
			if _, err := io.WriteString(w.destination, line+"\n"); err != nil {
				return written, err
			}
		}
		data = data[newline+1:]
	}
	return written, nil
}

func go2RTCLineLevel(line string, fallback opslog.Level) opslog.Level {
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, " ERR "):
		return opslog.Error
	case strings.Contains(upper, " WRN "):
		return opslog.Warn
	case strings.Contains(upper, " INF "):
		return opslog.Info
	case strings.Contains(upper, " DBG "):
		return opslog.Debug
	}
	if fallback < opslog.Debug || fallback > opslog.Error {
		return opslog.Info
	}
	return fallback
}
