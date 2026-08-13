package opslog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type rotatingWriter struct {
	path     string
	maxBytes int64
	retained int
	file     *os.File
	size     int64
	mu       sync.Mutex
}

func newRotatingWriter(path string, maxBytes int64, retained int) (*rotatingWriter, error) {
	path = filepath.Clean(path)
	if path == "." || filepath.Base(path) != DefaultFilename {
		return nil, fmt.Errorf("operational log filename must be %s", DefaultFilename)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create operational log directory: %w", err)
	}
	writer := &rotatingWriter{path: path, maxBytes: maxBytes, retained: retained}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (writer *rotatingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return 0, errors.New("operational log file is closed")
	}
	if int64(len(data)) > writer.maxBytes {
		return 0, fmt.Errorf("operational log record exceeds file limit")
	}
	if writer.size+int64(len(data)) > writer.maxBytes {
		if err := writer.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := writer.file.Write(data)
	writer.size += int64(written)
	if err != nil {
		return written, fmt.Errorf("append operational log: %w", err)
	}
	return written, nil
}

func (writer *rotatingWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func (writer *rotatingWriter) open() error {
	file, err := os.OpenFile(writer.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open operational log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure operational log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat operational log: %w", err)
	}
	writer.file = file
	writer.size = info.Size()
	if writer.size > writer.maxBytes {
		return writer.rotate()
	}
	return nil
}

func (writer *rotatingWriter) rotate() error {
	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			return fmt.Errorf("close operational log for rotation: %w", err)
		}
		writer.file = nil
	}
	if writer.retained == 1 {
		if err := os.Remove(writer.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove operational log for rotation: %w", err)
		}
	} else {
		for index := writer.retained - 1; index >= 1; index-- {
			destination := fmt.Sprintf("%s.%d", writer.path, index)
			_ = os.Remove(destination)
			source := writer.path
			if index > 1 {
				source = fmt.Sprintf("%s.%d", writer.path, index-1)
			}
			if err := os.Rename(source, destination); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("rotate operational log: %w", err)
			}
		}
	}
	writer.size = 0
	return writer.open()
}
