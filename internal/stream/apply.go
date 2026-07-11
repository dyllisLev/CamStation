package stream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"camstation/internal/store"
)

type policyStore interface {
	ListCameras(context.Context, bool) ([]store.Camera, error)
	MarkCameraPoliciesApplied(context.Context, []store.CameraPolicyApplySnapshot) error
	MarkCameraPolicyFailed(context.Context, int64, int64, string) error
}

type policyRuntime interface {
	PrepareConfig(context.Context, []byte) (runtimeConfigTransaction, error)
}

type runtimeConfigTransaction interface {
	Commit() error
	Rollback(context.Context) error
}

type recorderHandoff interface {
	SuspendActive() []store.Camera
	RestoreActive([]store.Camera) error
}

type ApplyCoordinator struct {
	db        policyStore
	runtime   policyRuntime
	recorders recorderHandoff
	mu        chan struct{}
}

type PolicyApplyResult struct {
	Applied bool   `json:"applied"`
	Pending bool   `json:"pending"`
	Error   string `json:"error,omitempty"`
}

func NewApplyCoordinator(db policyStore, runtime policyRuntime, recorders recorderHandoff) *ApplyCoordinator {
	return &ApplyCoordinator{db: db, runtime: runtime, recorders: recorders, mu: make(chan struct{}, 1)}
}

func (c *ApplyCoordinator) Apply(ctx context.Context) PolicyApplyResult {
	select {
	case c.mu <- struct{}{}:
		defer func() { <-c.mu }()
	case <-ctx.Done():
		return PolicyApplyResult{Error: ctx.Err().Error()}
	}
	for {
		cameras, err := c.db.ListCameras(ctx, true)
		if err != nil {
			return PolicyApplyResult{Error: err.Error()}
		}
		config, results, err := renderPolicyConfig(cameras, false)
		if err != nil {
			c.markFailed(ctx, cameras, err)
			return PolicyApplyResult{Pending: true, Error: err.Error()}
		}
		active := c.recorders.SuspendActive()
		runtimeTx, err := c.runtime.PrepareConfig(ctx, config)
		if err != nil {
			_ = c.recorders.RestoreActive(active)
			c.markFailed(ctx, cameras, err)
			return PolicyApplyResult{Pending: true, Error: err.Error()}
		}
		now := time.Now().UTC()
		snapshots := make([]store.CameraPolicyApplySnapshot, 0, len(cameras))
		for _, camera := range cameras {
			cameraResults := results[camera.ID]
			for i := range cameraResults {
				cameraResults[i].Verification.CheckedAt = now
			}
			snapshots = append(snapshots, store.CameraPolicyApplySnapshot{
				CameraID: camera.ID, Revision: camera.PolicyState.DesiredRevision, Results: cameraResults,
			})
		}
		if len(snapshots) > 0 {
			if err := c.db.MarkCameraPoliciesApplied(ctx, snapshots); err != nil {
				rollbackErr := runtimeTx.Rollback(ctx)
				_ = c.recorders.RestoreActive(active)
				c.markFailed(ctx, cameras, err)
				if rollbackErr != nil {
					err = fmt.Errorf("%v; runtime rollback failed: %w", err, rollbackErr)
				}
				return PolicyApplyResult{Pending: true, Error: err.Error()}
			}
		}
		commitErr := runtimeTx.Commit()
		fresh, err := c.db.ListCameras(ctx, true)
		if err != nil {
			_ = c.recorders.RestoreActive(active)
			if commitErr != nil {
				err = fmt.Errorf("%v; last-good commit failed: %w", err, commitErr)
			}
			return PolicyApplyResult{Applied: true, Error: err.Error()}
		}
		if err := c.recorders.RestoreActive(freshActiveCameras(active, fresh)); err != nil {
			return PolicyApplyResult{Applied: true, Error: err.Error()}
		}
		if commitErr != nil {
			return PolicyApplyResult{Applied: true, Error: commitErr.Error()}
		}
		if !newerRevisionExists(cameras, fresh) {
			return PolicyApplyResult{Applied: true}
		}
	}
}

func (c *ApplyCoordinator) markFailed(ctx context.Context, cameras []store.Camera, applyErr error) {
	for _, camera := range cameras {
		_ = c.db.MarkCameraPolicyFailed(ctx, camera.ID, camera.PolicyState.DesiredRevision, applyErr.Error())
	}
}

func freshActiveCameras(active, fresh []store.Camera) []store.Camera {
	byID := make(map[int64]store.Camera, len(fresh))
	for _, camera := range fresh {
		byID[camera.ID] = camera
	}
	result := make([]store.Camera, 0, len(active))
	for _, previous := range active {
		if camera, ok := byID[previous.ID]; ok {
			result = append(result, camera)
		}
	}
	return result
}

func newerRevisionExists(before, after []store.Camera) bool {
	revisions := make(map[int64]int64, len(before))
	for _, camera := range before {
		revisions[camera.ID] = camera.PolicyState.DesiredRevision
	}
	for _, camera := range after {
		if previous, ok := revisions[camera.ID]; !ok || camera.PolicyState.DesiredRevision > previous {
			return true
		}
	}
	return false
}

func (g *Go2RTC) ApplyConfig(ctx context.Context, config []byte) error {
	tx, err := g.PrepareConfig(ctx, config)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (g *Go2RTC) applyConfig(ctx context.Context, config []byte, restart func(context.Context) error) error {
	tx, err := g.prepareConfig(ctx, config, restart)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (g *Go2RTC) PrepareConfig(ctx context.Context, config []byte) (runtimeConfigTransaction, error) {
	return g.prepareConfig(ctx, config, g.restartProcess)
}

func (g *Go2RTC) prepareConfig(ctx context.Context, config []byte, restart func(context.Context) error) (runtimeConfigTransaction, error) {
	g.applyMu.Lock()
	if err := os.MkdirAll(filepath.Dir(g.configPath), 0o755); err != nil {
		g.applyMu.Unlock()
		return nil, err
	}
	previous, previousErr := os.ReadFile(g.configPath)
	if errors.Is(previousErr, os.ErrNotExist) {
		previous, previousErr = os.ReadFile(g.configPath + ".last-good")
	}
	if previousErr != nil && !errors.Is(previousErr, os.ErrNotExist) {
		g.applyMu.Unlock()
		return nil, previousErr
	}
	if len(previous) > 0 {
		if _, err := os.Stat(g.configPath + ".last-good"); errors.Is(err, os.ErrNotExist) {
			if err := writeFileAtomic(g.configPath+".last-good", previous); err != nil {
				g.applyMu.Unlock()
				return nil, err
			}
		} else if err != nil {
			g.applyMu.Unlock()
			return nil, err
		}
	}
	if err := writeFileAtomic(g.configPath, config); err != nil {
		g.applyMu.Unlock()
		return nil, err
	}
	if err := restart(ctx); err != nil {
		if len(previous) == 0 {
			_ = os.Remove(g.configPath)
			g.applyMu.Unlock()
			return nil, err
		}
		if restoreErr := writeFileAtomic(g.configPath, previous); restoreErr != nil {
			g.applyMu.Unlock()
			return nil, fmt.Errorf("apply failed: %v; restore config failed: %w", err, restoreErr)
		}
		if restoreErr := restart(ctx); restoreErr != nil {
			g.applyMu.Unlock()
			return nil, fmt.Errorf("apply failed: %v; restore process failed: %w", err, restoreErr)
		}
		g.applyMu.Unlock()
		return nil, err
	}
	return &go2RTCConfigTransaction{g: g, previous: previous, current: append([]byte(nil), config...), restart: restart}, nil
}

type go2RTCConfigTransaction struct {
	g                 *Go2RTC
	previous, current []byte
	restart           func(context.Context) error
	done              bool
}

func (t *go2RTCConfigTransaction) Commit() error {
	if t.done {
		return fmt.Errorf("config transaction already completed")
	}
	t.done = true
	defer t.g.applyMu.Unlock()
	return writeFileAtomic(t.g.configPath+".last-good", t.current)
}

func (t *go2RTCConfigTransaction) Rollback(ctx context.Context) error {
	if t.done {
		return fmt.Errorf("config transaction already completed")
	}
	t.done = true
	defer t.g.applyMu.Unlock()
	if len(t.previous) == 0 {
		return fmt.Errorf("no previous config to restore")
	}
	if err := writeFileAtomic(t.g.configPath, t.previous); err != nil {
		return err
	}
	return t.restart(ctx)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempName := file.Name()
	defer os.Remove(tempName)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
