package viewerinstall

import (
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

type CommitMarker struct {
	SchemaVersion int    `json:"schemaVersion"`
	TransactionID string `json:"transactionId"`
	CommandID     int64  `json:"commandId"`
	PayloadHash   string `json:"payloadHash"`
	Version       string `json:"version"`
	Digest        string `json:"digest"`
	Generation    int64  `json:"generation"`
	Token         string `json:"token"`
}

func SaveCommitMarker(layout Layout, marker CommitMarker) error {
	marker.SchemaVersion = SchemaVersion
	marker.Digest = strings.ToLower(strings.TrimSpace(marker.Digest))
	marker.Token = strings.ToLower(strings.TrimSpace(marker.Token))
	if err := validateCommitMarker(marker); err != nil {
		return err
	}
	return atomicWriteJSON(layout.CommitMarkerPath(), marker)
}

func LoadCommitMarker(layout Layout) (CommitMarker, error) {
	var marker CommitMarker
	if err := readJSON(layout.CommitMarkerPath(), &marker); err != nil {
		return CommitMarker{}, err
	}
	if err := validateCommitMarker(marker); err != nil {
		return CommitMarker{}, err
	}
	return marker, nil
}

func RemoveCommitMarker(layout Layout) error {
	err := os.Remove(layout.CommitMarkerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDir(layout.StateDir)
}

func commitMarkerMatches(marker CommitMarker, journal Journal) bool {
	return marker.TransactionID == journal.TransactionID && marker.CommandID == journal.CommandID &&
		marker.PayloadHash == journal.PayloadHash && marker.Version == journal.Release.Version &&
		marker.Digest == journal.Release.Digest && marker.Generation == journal.Generation
}

func validateCommitMarker(marker CommitMarker) error {
	if marker.SchemaVersion != SchemaVersion || !validVersion(marker.TransactionID) || marker.CommandID <= 0 ||
		strings.TrimSpace(marker.PayloadHash) == "" || !validVersion(marker.Version) || !validDigest(marker.Digest) ||
		marker.Generation <= 0 || len(marker.Token) != 64 || strings.ToLower(marker.Token) != marker.Token {
		return errors.New("invalid update commit marker")
	}
	if _, err := hex.DecodeString(marker.Token); err != nil {
		return errors.New("invalid update commit token")
	}
	return nil
}
