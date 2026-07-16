package vieweragent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const MaxStateFileBytes int64 = 1024 * 1024

type MachinePaths struct {
	Config   string
	State    string
	Commands string
	Update   string
}

func PathsFromConfig(configPath string) MachinePaths {
	dir := filepath.Dir(configPath)
	return MachinePaths{
		Config:   configPath,
		State:    filepath.Join(dir, "state.json"),
		Commands: filepath.Join(dir, "commands.json"),
		Update:   filepath.Join(dir, "update.json"),
	}
}

type MachineState struct {
	SchemaVersion            int         `json:"schemaVersion"`
	ClientID                 string      `json:"clientId,omitempty"`
	AgentBootGeneration      int64       `json:"agentBootGeneration"`
	ViewerGeneration         int64       `json:"viewerGeneration"`
	ExpectedViewerGeneration int64       `json:"expectedViewerGeneration"`
	ViewerNonce              string      `json:"viewerNonce,omitempty"`
	ExpectedViewerPID        int         `json:"expectedViewerPid,omitempty"`
	ExpectedViewerSession    uint32      `json:"expectedViewerSession,omitempty"`
	ControlState             string      `json:"controlState,omitempty"`
	CommandEngineHealthy     bool        `json:"commandEngineHealthy"`
	LastControlSuccessAt     *time.Time  `json:"lastControlSuccessAt,omitempty"`
	LastHeartbeatAt          *time.Time  `json:"lastHeartbeatAt,omitempty"`
	ViewerState              string      `json:"viewerState,omitempty"`
	ViewerLastHeartbeatAt    *time.Time  `json:"viewerLastHeartbeatAt,omitempty"`
	RendererState            string      `json:"rendererState,omitempty"`
	RendererLastHeartbeatAt  *time.Time  `json:"rendererLastHeartbeatAt,omitempty"`
	ViewerRestartHistory     []time.Time `json:"viewerRestartHistory,omitempty"`
	ForcedViewerRestartID    string      `json:"forcedViewerRestartId,omitempty"`
	ForcedViewerRestartAt    *time.Time  `json:"forcedViewerRestartAt,omitempty"`
}

func LoadMachineState(path string) (MachineState, error) {
	var state MachineState
	err := readBoundedJSON(path, &state)
	if errors.Is(err, os.ErrNotExist) {
		return MachineState{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return MachineState{}, err
	}
	if state.SchemaVersion != SchemaVersion {
		return MachineState{}, errors.New("unsupported machine state schema")
	}
	return state, nil
}

func SaveMachineState(path string, state MachineState) error {
	state.SchemaVersion = SchemaVersion
	return atomicWriteJSON(path, state)
}

func (state *MachineState) AllowViewerRestart(now time.Time, forced bool, commandID string) (bool, int64) {
	cutoff := now.Add(-time.Hour)
	kept := state.ViewerRestartHistory[:0]
	for _, at := range state.ViewerRestartHistory {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	state.ViewerRestartHistory = kept
	if forced {
		if commandID == "" {
			return false, state.ExpectedViewerGeneration
		}
		if state.ForcedViewerRestartID == commandID {
			return false, state.ExpectedViewerGeneration
		}
		state.ForcedViewerRestartID = commandID
		state.ForcedViewerRestartAt = &now
	} else {
		if len(state.ViewerRestartHistory) >= 3 {
			return false, state.ExpectedViewerGeneration
		}
		if count := len(state.ViewerRestartHistory); count > 0 && now.Sub(state.ViewerRestartHistory[count-1]) < 10*time.Minute {
			return false, state.ExpectedViewerGeneration
		}
		state.ViewerRestartHistory = append(state.ViewerRestartHistory, now)
	}
	next := state.ViewerGeneration + 1
	if state.ExpectedViewerGeneration >= next {
		next = state.ExpectedViewerGeneration + 1
	}
	state.ExpectedViewerGeneration = next
	return true, next
}

type CommandState string

const (
	CommandReceived  CommandState = "received"
	CommandRunning   CommandState = "running"
	CommandSucceeded CommandState = "succeeded"
	CommandFailed    CommandState = "failed"
	CommandRejected  CommandState = "rejected"
	CommandExpired   CommandState = "expired"
)

func (state CommandState) terminal() bool {
	return state == CommandSucceeded || state == CommandFailed || state == CommandRejected || state == CommandExpired
}

type CommandRecord struct {
	ID             int64        `json:"id"`
	Type           string       `json:"type"`
	PayloadHash    string       `json:"payloadHash"`
	OperationKey   string       `json:"operationKey,omitempty"`
	DesiredVersion string       `json:"desiredVersion,omitempty"`
	ArtifactSHA256 string       `json:"artifactSha256,omitempty"`
	Generation     int64        `json:"generation"`
	State          CommandState `json:"state"`
	Error          string       `json:"error,omitempty"`
	CreatedAt      time.Time    `json:"createdAt"`
	TTLSeconds     int          `json:"ttlSeconds"`
	ReceivedAt     time.Time    `json:"receivedAt"`
	RunningAt      *time.Time   `json:"runningAt,omitempty"`
	CompletedAt    *time.Time   `json:"completedAt,omitempty"`
}

type CommandLedger struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Records       map[string]CommandRecord `json:"records"`
}

func LoadCommandLedger(path string) (CommandLedger, error) {
	var ledger CommandLedger
	err := readBoundedJSON(path, &ledger)
	if errors.Is(err, os.ErrNotExist) {
		return CommandLedger{SchemaVersion: SchemaVersion, Records: make(map[string]CommandRecord)}, nil
	}
	if err != nil {
		return CommandLedger{}, err
	}
	if ledger.SchemaVersion != SchemaVersion {
		return CommandLedger{}, errors.New("unsupported command ledger schema")
	}
	if ledger.Records == nil {
		ledger.Records = make(map[string]CommandRecord)
	}
	return ledger, nil
}

func SaveCommandLedger(path string, ledger CommandLedger) error {
	ledger.SchemaVersion = SchemaVersion
	if ledger.Records == nil {
		ledger.Records = make(map[string]CommandRecord)
	}
	return atomicWriteJSON(path, ledger)
}

type QuarantinedUpdate struct {
	Version    string    `json:"version"`
	SHA256     string    `json:"sha256"`
	Generation int64     `json:"generation"`
	At         time.Time `json:"at"`
	Reason     string    `json:"reason"`
}

type UpdateJournal struct {
	SchemaVersion  int                 `json:"schemaVersion"`
	State          string              `json:"state,omitempty"`
	TargetVersion  string              `json:"targetVersion,omitempty"`
	ArtifactSHA256 string              `json:"artifactSha256,omitempty"`
	Generation     int64               `json:"generation"`
	Quarantined    []QuarantinedUpdate `json:"quarantined,omitempty"`
}

func (journal *UpdateJournal) Quarantine(version, digest string, generation int64, at time.Time, reason string) {
	if journal.IsQuarantined(version, digest, generation) {
		return
	}
	journal.Quarantined = append(journal.Quarantined, QuarantinedUpdate{
		Version: version, SHA256: strings.ToLower(digest), Generation: generation, At: at, Reason: reason,
	})
}

func (journal UpdateJournal) IsQuarantined(version, digest string, generation int64) bool {
	for _, failed := range journal.Quarantined {
		if failed.Version == version && failed.SHA256 == strings.ToLower(digest) && failed.Generation == generation {
			return true
		}
	}
	return false
}

func LoadUpdateJournal(path string) (UpdateJournal, error) {
	var journal UpdateJournal
	err := readBoundedJSON(path, &journal)
	if errors.Is(err, os.ErrNotExist) {
		return UpdateJournal{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return UpdateJournal{}, err
	}
	if journal.SchemaVersion != SchemaVersion {
		return UpdateJournal{}, errors.New("unsupported update journal schema")
	}
	return journal, nil
}

func SaveUpdateJournal(path string, journal UpdateJournal) error {
	journal.SchemaVersion = SchemaVersion
	return atomicWriteJSON(path, journal)
}

func readBoundedJSON(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxStateFileBytes {
		return errors.New("state file is not a bounded regular file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, MaxStateFileBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("state file contains trailing data")
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if int64(len(encoded)) > MaxStateFileBytes {
		return errors.New("state file exceeds size limit")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, bytes.NewReader(encoded)); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

func commandKey(id int64) string { return strconv.FormatInt(id, 10) }
