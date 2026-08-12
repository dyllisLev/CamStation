package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"camstation/internal/store"
)

func waitForBackupState(t *testing.T, db *store.DB, id int64, want store.JobState) store.Job {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := db.GetJob(t.Context(), id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if job.State == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := db.GetJob(t.Context(), id)
	if err != nil {
		t.Fatalf("get final job: %v", err)
	}
	t.Fatalf("job %d state = %s, want %s", id, job.State, want)
	return store.Job{}
}

func mustMarshalString(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(encoded)
}

func createBackupFixture(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
