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
	Applied          bool   `json:"applied"`
	Pending          bool   `json:"pending"`
	RecoveryFailed   bool   `json:"recoveryFailed,omitempty"`
	RevisionConflict bool   `json:"revisionConflict,omitempty"`
	Error            string `json:"error,omitempty"`
}

func NewApplyCoordinator(db policyStore, runtime policyRuntime, recorders recorderHandoff) *ApplyCoordinator {
	return &ApplyCoordinator{db: db, runtime: runtime, recorders: recorders, mu: make(chan struct{}, 1)}
}

func (c *ApplyCoordinator) Apply(ctx context.Context) PolicyApplyResult {
	return c.apply(ctx, nil)
}

type expectedCameraRevision struct {
	cameraID, revision int64
}

func (c *ApplyCoordinator) ApplyExpected(ctx context.Context, cameraID, revision int64) PolicyApplyResult {
	return c.apply(ctx, &expectedCameraRevision{cameraID: cameraID, revision: revision})
}

func (c *ApplyCoordinator) apply(ctx context.Context, expected *expectedCameraRevision) PolicyApplyResult {
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
		if expected != nil {
			matched := false
			for _, camera := range cameras {
				if camera.ID == expected.cameraID {
					matched = camera.PolicyState.DesiredRevision == expected.revision
					break
				}
			}
			if !matched {
				return PolicyApplyResult{RevisionConflict: true, Error: store.ErrDesiredRevisionMismatch.Error()}
			}
			expected = nil
		}
		config, results, err := renderPolicyConfig(cameras, false)
		if err != nil {
			c.markFailed(ctx, cameras, err)
			return PolicyApplyResult{Pending: true, Error: err.Error()}
		}
		active := c.recorders.SuspendActive()
		runtimeTx, err := c.runtime.PrepareConfig(ctx, config)
		if err != nil {
			restoreErr := c.recorders.RestoreActive(active)
			c.markFailed(ctx, cameras, err)
			return PolicyApplyResult{Pending: true, RecoveryFailed: runtimeRecoveryFailed(err) || restoreErr != nil, Error: handoffError(err, nil, restoreErr).Error()}
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
		appliedCameras := camerasWithAppliedResults(cameras, results, now)
		if err := c.recorders.RestoreActive(freshActiveCameras(active, appliedCameras)); err != nil {
			_ = c.recorders.SuspendActive()
			rollbackErr := runtimeTx.Rollback(ctx)
			restoreErr := c.recorders.RestoreActive(active)
			c.markFailed(ctx, cameras, err)
			return PolicyApplyResult{Pending: true, RecoveryFailed: rollbackErr != nil || restoreErr != nil, Error: handoffError(err, rollbackErr, restoreErr).Error()}
		}
		if len(snapshots) > 0 {
			if err := c.db.MarkCameraPoliciesApplied(ctx, snapshots); err != nil {
				_ = c.recorders.SuspendActive()
				rollbackErr := runtimeTx.Rollback(ctx)
				restoreErr := c.recorders.RestoreActive(active)
				c.markFailed(ctx, cameras, err)
				return PolicyApplyResult{Pending: true, RecoveryFailed: rollbackErr != nil || restoreErr != nil, Error: handoffError(err, rollbackErr, restoreErr).Error()}
			}
		}
		commitErr := runtimeTx.Commit()
		fresh, err := c.db.ListCameras(ctx, true)
		if err != nil {
			if commitErr != nil {
				err = fmt.Errorf("%v; last-good commit failed: %w", err, commitErr)
			}
			applied := commitErr == nil || lastGoodInvariantPreserved(commitErr)
			return PolicyApplyResult{Applied: applied, RecoveryFailed: !applied, Error: err.Error()}
		}
		if commitErr != nil {
			applied := lastGoodInvariantPreserved(commitErr)
			return PolicyApplyResult{Applied: applied, RecoveryFailed: !applied, Error: commitErr.Error()}
		}
		if !newerRevisionExists(cameras, fresh) {
			return PolicyApplyResult{Applied: true}
		}
	}
}

func camerasWithAppliedResults(cameras []store.Camera, results map[int64][]store.CameraOutputApplyResult, appliedAt time.Time) []store.Camera {
	applied := make([]store.Camera, len(cameras))
	for i, camera := range cameras {
		camera.Outputs = append([]store.CameraOutput(nil), camera.Outputs...)
		byPurpose := make(map[store.CameraOutputPurpose]store.CameraOutputApplyResult, len(results[camera.ID]))
		for _, result := range results[camera.ID] {
			byPurpose[result.Purpose] = result
		}
		for j := range camera.Outputs {
			if result, ok := byPurpose[camera.Outputs[j].Purpose]; ok {
				camera.Outputs[j].AppliedPolicy = result.Policy
				camera.Outputs[j].Verification = result.Verification
			}
		}
		camera.PolicyState.AppliedRevision = camera.PolicyState.DesiredRevision
		camera.PolicyState.ApplyState = store.CameraApplyApplied
		camera.PolicyState.ApplyStateAt = appliedAt
		camera.PolicyState.AppliedAt = appliedAt
		camera.PolicyState.ApplyError = ""
		applied[i] = camera
	}
	return applied
}

func handoffError(primary, rollback, restore error) error {
	if rollback != nil {
		primary = fmt.Errorf("%v; runtime rollback failed: %w", primary, rollback)
	}
	if restore != nil {
		primary = fmt.Errorf("%v; recorder restore failed: %w", primary, restore)
	}
	return primary
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
			return nil, &runtimeRecoveryError{err: fmt.Errorf("apply failed: %v; restore config failed: %w", err, restoreErr)}
		}
		if restoreErr := restart(ctx); restoreErr != nil {
			g.applyMu.Unlock()
			return nil, &runtimeRecoveryError{err: fmt.Errorf("apply failed: %v; restore process failed: %w", err, restoreErr)}
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

type lastGoodCommitError struct {
	err           error
	invariantSafe bool
}

type runtimeRecoveryError struct{ err error }

func (e *runtimeRecoveryError) Error() string { return e.err.Error() }
func (e *runtimeRecoveryError) Unwrap() error { return e.err }

func runtimeRecoveryFailed(err error) bool {
	var recoveryErr *runtimeRecoveryError
	return errors.As(err, &recoveryErr)
}

func (e *lastGoodCommitError) Error() string { return e.err.Error() }
func (e *lastGoodCommitError) Unwrap() error { return e.err }

func lastGoodInvariantPreserved(err error) bool {
	var commitErr *lastGoodCommitError
	return errors.As(err, &commitErr) && commitErr.invariantSafe
}

func (t *go2RTCConfigTransaction) Commit() error {
	if t.done {
		return fmt.Errorf("config transaction already completed")
	}
	t.done = true
	defer t.g.applyMu.Unlock()
	lastGoodPath := t.g.configPath + ".last-good"
	if err := writeFileAtomic(lastGoodPath, t.current); err != nil {
		invalidateErr := os.Remove(lastGoodPath)
		if invalidateErr == nil || errors.Is(invalidateErr, os.ErrNotExist) {
			return &lastGoodCommitError{err: err, invariantSafe: true}
		}
		return &lastGoodCommitError{
			err:           fmt.Errorf("%v; stale last-good invalidation failed: %w", err, invalidateErr),
			invariantSafe: false,
		}
	}
	return nil
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
