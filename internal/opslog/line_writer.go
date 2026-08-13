package opslog

import (
	"bytes"
	"sync"
)

const maxLineWriterBytes = 64 << 10

type lineWriter struct {
	logger    *Logger
	component string
	level     Level

	mu       sync.Mutex
	pending  []byte
	dropping bool
}

func NewLineWriter(logger *Logger, component string, level Level) *lineWriter {
	return &lineWriter{logger: logger, component: component, level: level}
}

func (writer *lineWriter) Write(data []byte) (int, error) {
	written := len(data)
	writer.mu.Lock()
	defer writer.mu.Unlock()
	for len(data) > 0 {
		if writer.dropping {
			newline := bytes.IndexByte(data, '\n')
			if newline < 0 {
				return written, nil
			}
			writer.dropping = false
			data = data[newline+1:]
			continue
		}
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			if len(writer.pending)+len(data) > maxLineWriterBytes {
				writer.pending = nil
				writer.dropping = true
				if writer.logger != nil {
					_ = writer.logger.Log(Warn, writer.component, "line_dropped", Fields{ErrorCode: "line_too_long"})
				}
				return written, nil
			}
			writer.pending = append(writer.pending, data...)
			return written, nil
		}
		writer.pending = append(writer.pending, data[:newline]...)
		if writer.logger != nil && len(writer.pending) > 0 {
			if err := writer.logger.Log(writer.level, writer.component, "message", Fields{Message: string(writer.pending)}); err != nil {
				return written, err
			}
		}
		writer.pending = writer.pending[:0]
		data = data[newline+1:]
	}
	return written, nil
}
