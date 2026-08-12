package viewerservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	CommandJournalSchemaVersion       = 1
	MaxCommandJournalBytes      int64 = 1024 * 1024
	DefaultCommandJournalPath         = `C:\ProgramData\CamStation\Viewer\commands.json`
)

type LocalCommandRecord struct {
	ID                     int64      `json:"id"`
	Type                   string     `json:"type"`
	PayloadHash            string     `json:"payloadHash"`
	OperationKey           string     `json:"operationKey"`
	State                  string     `json:"state"`
	Error                  string     `json:"error,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	TTLSeconds             int        `json:"ttlSeconds"`
	AcceptedAt             time.Time  `json:"acceptedAt"`
	AcceptedBootGeneration int64      `json:"acceptedBootGeneration"`
	RunningAt              *time.Time `json:"runningAt,omitempty"`
	CompletedAt            *time.Time `json:"completedAt,omitempty"`
	TargetBootGeneration   int64      `json:"targetBootGeneration,omitempty"`
	ResultReportedAt       *time.Time `json:"resultReportedAt,omitempty"`
}

type CommandJournal struct {
	SchemaVersion  int                           `json:"schemaVersion"`
	BootGeneration int64                         `json:"bootGeneration"`
	Records        map[string]LocalCommandRecord `json:"records"`
}

type CommandJournalStore interface {
	Load() (CommandJournal, error)
	Save(CommandJournal) error
}

type FileCommandJournalStore struct {
	Path string
}

func (store FileCommandJournalStore) Load() (CommandJournal, error) {
	path := filepath.Clean(store.Path)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyCommandJournal(), nil
	}
	if err != nil {
		return CommandJournal{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return CommandJournal{}, err
	}
	if info.Size() > MaxCommandJournalBytes {
		return CommandJournal{}, errors.New("command journal exceeds size limit")
	}
	var journal CommandJournal
	decoder := json.NewDecoder(io.LimitReader(file, MaxCommandJournalBytes+1))
	if err := decoder.Decode(&journal); err != nil {
		return CommandJournal{}, fmt.Errorf("decode command journal: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CommandJournal{}, errors.New("command journal contains trailing data")
	}
	if journal.SchemaVersion != CommandJournalSchemaVersion {
		return CommandJournal{}, errors.New("unsupported command journal schema")
	}
	if journal.Records == nil {
		journal.Records = make(map[string]LocalCommandRecord)
	}
	return journal, nil
}

func (store FileCommandJournalStore) Save(journal CommandJournal) error {
	journal.SchemaVersion = CommandJournalSchemaVersion
	if journal.Records == nil {
		journal.Records = make(map[string]LocalCommandRecord)
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if int64(len(encoded)) > MaxCommandJournalBytes {
		return errors.New("command journal exceeds size limit")
	}
	path := filepath.Clean(store.Path)
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".commands-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(encoded)); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := secureCommandJournalFile(temporaryPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace command journal: %w", err)
	}
	return nil
}

type MemoryCommandJournalStore struct {
	mu      sync.Mutex
	journal CommandJournal
}

func (store *MemoryCommandJournalStore) Load() (CommandJournal, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	journal := store.journal
	if journal.SchemaVersion == 0 {
		journal = emptyCommandJournal()
	}
	journal.Records = cloneCommandRecords(journal.Records)
	return journal, nil
}

func (store *MemoryCommandJournalStore) Save(journal CommandJournal) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	journal.SchemaVersion = CommandJournalSchemaVersion
	journal.Records = cloneCommandRecords(journal.Records)
	store.journal = journal
	return nil
}

func emptyCommandJournal() CommandJournal {
	return CommandJournal{SchemaVersion: CommandJournalSchemaVersion, Records: make(map[string]LocalCommandRecord)}
}

func cloneCommandRecords(records map[string]LocalCommandRecord) map[string]LocalCommandRecord {
	result := make(map[string]LocalCommandRecord, len(records))
	for key, record := range records {
		result[key] = record
	}
	return result
}
