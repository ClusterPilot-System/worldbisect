package jobs

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestConcurrentWorkersExecuteJobOnce(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(dataStore, 8, 200*time.Millisecond, 3)
	var calls atomic.Int32
	manager.Register("test", func(ctx context.Context, job *model.Job) (string, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return "ok", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	manager.Run(ctx)
	payload, _ := json.Marshal(map[string]string{"value": "x"})
	job, _, err := manager.Enqueue("test", payload, "test", "fingerprint", "idempotency-123", "digest")
	if err != nil {
		t.Fatal(err)
	}
	manager.Notify()
	manager.Notify()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := dataStore.LoadJob(job.ID)
		if err == nil && current.State == model.JobSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	manager.Wait()
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestIdempotentEnqueue(t *testing.T) {
	dataStore, _ := store.Open(filepath.Join(t.TempDir(), "store"))
	manager := New(dataStore, 1, time.Second, 3)
	first, existing, err := manager.Enqueue("test", []byte("{}"), "user", "fingerprint", "same-key", "same-digest")
	if err != nil || existing {
		t.Fatalf("first enqueue: existing=%t err=%v", existing, err)
	}
	second, existing, err := manager.Enqueue("test", []byte("{}"), "user", "fingerprint", "same-key", "same-digest")
	if err != nil || !existing || second.ID != first.ID {
		t.Fatalf("second enqueue: existing=%t err=%v IDs %s %s", existing, err, first.ID, second.ID)
	}
	if _, _, err := manager.Enqueue("test", []byte("other"), "user", "fingerprint", "same-key", "other-digest"); err == nil {
		t.Fatal("expected idempotency conflict")
	}
}
