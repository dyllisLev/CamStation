package store

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

type DB struct {
	db               *sql.DB
	readDB           *sql.DB
	recordingMediaMu sync.Mutex
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	databaseURL := url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}
	db, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Keep one writer and its existing transaction/foreign-key semantics. WAL
	// readers see committed data without queueing behind recording publication
	// or its fsync/checkpoint. Apply read-only settings to every pooled connection.
	databaseURL.RawQuery = url.Values{
		"mode":    {"ro"},
		"_pragma": {"query_only(1)", "busy_timeout(1000)"},
	}.Encode()
	readDB, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		db.Close()
		return nil, err
	}
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(4)
	return &DB{db: db, readDB: readDB}, nil
}

func (d *DB) Close() error {
	return errors.Join(d.readDB.Close(), d.db.Close())
}

type scanner interface {
	Scan(dest ...any) error
}
