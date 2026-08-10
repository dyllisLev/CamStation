package legacyimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	modernsqlite "modernc.org/sqlite"
)

type onlineBackuper interface {
	NewBackup(string) (*modernsqlite.Backup, error)
}

func Snapshot(ctx context.Context, source, target string, expectations Expectations) (Manifest, error) {
	manifest := Manifest{FormatVersion: ManifestVersion, Operation: "snapshot"}
	if strings.TrimSpace(target) == "" {
		return manifest, errors.New("snapshot target path is required")
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return manifest, fmt.Errorf("resolve source path: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return manifest, fmt.Errorf("resolve snapshot target path: %w", err)
	}
	if sourceAbs == targetAbs {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SNAPSHOT_EQUALS_SOURCE", Message: "snapshot target must differ from the active 1.x database"})
		manifest.TargetStatus = "rejected"
		return manifest, nil
	}
	if _, err := os.Lstat(targetAbs); err == nil {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SNAPSHOT_TARGET_EXISTS", Message: "an immutable snapshot target already exists and was not overwritten"})
		manifest.TargetStatus = "rejected"
		return manifest, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return manifest, fmt.Errorf("inspect snapshot target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o750); err != nil {
		return manifest, fmt.Errorf("create snapshot directory: %w", err)
	}
	placeholder, err := os.CreateTemp(filepath.Dir(targetAbs), ".camstation-snapshot-*.db")
	if err != nil {
		return manifest, fmt.Errorf("reserve snapshot path: %w", err)
	}
	staged := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		cleanupSQLiteFiles(staged)
		return manifest, fmt.Errorf("close snapshot placeholder: %w", err)
	}
	if err := os.Remove(staged); err != nil {
		cleanupSQLiteFiles(staged)
		return manifest, fmt.Errorf("prepare snapshot destination: %w", err)
	}
	defer cleanupSQLiteFiles(staged)

	sourceDB, err := openLegacyReadOnly(ctx, sourceAbs)
	if err != nil {
		return manifest, err
	}
	connection, err := sourceDB.Conn(ctx)
	if err != nil {
		sourceDB.Close()
		return manifest, fmt.Errorf("acquire source snapshot connection: %w", err)
	}
	err = connection.Raw(func(driverConnection any) error {
		backuper, ok := driverConnection.(onlineBackuper)
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}
		backup, err := backuper.NewBackup(staged)
		if err != nil {
			return fmt.Errorf("initialize online backup: %w", err)
		}
		_, stepErr := backup.Step(-1)
		finishErr := backup.Finish()
		if stepErr != nil {
			return fmt.Errorf("copy online backup pages: %w", stepErr)
		}
		if finishErr != nil {
			return fmt.Errorf("finish online backup: %w", finishErr)
		}
		return nil
	})
	connection.Close()
	sourceDB.Close()
	if err != nil {
		return manifest, err
	}
	if err := os.Chmod(staged, 0o600); err != nil {
		return manifest, fmt.Errorf("protect staged snapshot: %w", err)
	}

	prepared, err := buildPlan(ctx, staged, expectations)
	prepared.manifest.Operation = "snapshot"
	if err != nil {
		return prepared.manifest, err
	}
	if !prepared.manifest.Ready {
		prepared.manifest.TargetStatus = "rejected"
		return prepared.manifest, nil
	}
	if err := syncFile(staged); err != nil {
		return prepared.manifest, fmt.Errorf("sync staged snapshot: %w", err)
	}
	if err := os.Link(staged, targetAbs); err != nil {
		if errors.Is(err, os.ErrExist) {
			prepared.manifest.Ready = false
			prepared.manifest.TargetStatus = "rejected"
			prepared.manifest.Blockers = append(prepared.manifest.Blockers, Finding{Code: "SNAPSHOT_TARGET_RACE", Message: "snapshot target appeared during backup and was not overwritten"})
			return prepared.manifest, nil
		}
		return prepared.manifest, fmt.Errorf("promote immutable snapshot: %w", err)
	}
	if err := os.Remove(staged); err != nil {
		return prepared.manifest, fmt.Errorf("remove staged snapshot link: %w", err)
	}
	if err := syncDirectory(filepath.Dir(targetAbs)); err != nil {
		return prepared.manifest, fmt.Errorf("sync snapshot directory: %w", err)
	}
	prepared.manifest.TargetStatus = "created"
	return prepared.manifest, nil
}
