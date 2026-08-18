package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func TestBlobRoundTrip(t *testing.T) {
	dataStore, err := Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := dataStore.PutBlob([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := dataStore.GetBlob(digest)
	if err != nil || string(content) != "hello" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestAtomicJobClaim(t *testing.T) {
	dataStore, _ := Open(filepath.Join(t.TempDir(), "store"))
	job := model.Job{SchemaVersion: 3, ID: "job-1", Type: "test", State: model.JobQueued, Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := dataStore.SaveJob(&job); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			claimed, err := dataStore.ClaimNextJob(string(rune('a'+worker)), time.Second, 3)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if claimed != nil {
				claims.Add(1)
			}
		}(index)
	}
	wait.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claims = %d", claims.Load())
	}
}

func TestLeaseOwnership(t *testing.T) {
	dataStore, _ := Open(filepath.Join(t.TempDir(), "store"))
	job := model.Job{ID: "job-lease", Type: "test", State: model.JobQueued, Payload: json.RawMessage(`{}`)}
	if err := dataStore.SaveJob(&job); err != nil {
		t.Fatal(err)
	}
	claimed, err := dataStore.ClaimNextJob("worker-a", time.Second, 3)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if err := dataStore.HeartbeatJob(claimed.ID, "worker-b", time.Second); err == nil {
		t.Fatal("non-owner heartbeat accepted")
	}
	if err := dataStore.CompleteJob(claimed.ID, "worker-b", model.JobSucceeded, "", ""); err == nil {
		t.Fatal("non-owner completion accepted")
	}
}

func TestRequeueExpiredLease(t *testing.T) {
	dataStore, _ := Open(filepath.Join(t.TempDir(), "store"))
	job := model.Job{ID: "job-expired", Type: "test", State: model.JobRunning, LeaseOwner: "dead", LeaseExpires: time.Now().Add(-time.Second), Attempts: 1}
	if err := dataStore.SaveJob(&job); err != nil {
		t.Fatal(err)
	}
	count, err := dataStore.RequeueExpiredJobs(3)
	if err != nil || count != 1 {
		t.Fatalf("requeue count=%d err=%v", count, err)
	}
	current, _ := dataStore.LoadJob(job.ID)
	if current.State != model.JobQueued || current.LeaseOwner != "" {
		t.Fatalf("job=%+v", current)
	}
}

func TestAuditTamperDetection(t *testing.T) {
	dataStore, _ := Open(filepath.Join(t.TempDir(), "store"))
	if err := dataStore.AppendAudit("test", "capture", "one", nil); err != nil {
		t.Fatal(err)
	}
	verification, err := dataStore.VerifyAudit()
	if err != nil || !verification.Valid {
		t.Fatalf("verification=%+v err=%v", verification, err)
	}
	path := filepath.Join(dataStore.Root(), "audit", "events.jsonl")
	content, _ := os.ReadFile(path)
	content[len(content)/2] ^= 1
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
	verification, err = dataStore.VerifyAudit()
	if err != nil || verification.Valid {
		t.Fatalf("tamper not detected: %+v %v", verification, err)
	}
}

func TestSchemaMigration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "schema.json"), []byte("{\"version\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err != nil {
		t.Fatal(err)
	}
	var schema schemaFile
	content, _ := os.ReadFile(filepath.Join(root, "schema.json"))
	if err := json.Unmarshal(content, &schema); err != nil || schema.Version != currentSchemaVersion {
		t.Fatalf("schema=%+v err=%v", schema, err)
	}
}
