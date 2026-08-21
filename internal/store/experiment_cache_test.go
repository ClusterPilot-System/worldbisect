package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func TestExperimentCacheRoundTripAndInvalidation(t *testing.T) {
	dataStore, err := Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	want := model.Experiment{ID: "exp-1", Kind: "baseline", OracleResult: model.OracleResult{Passed: true}}
	if err := dataStore.SaveExperimentCache(key, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := dataStore.LoadExperimentCache(key)
	if err != nil || !found || got.ID != want.ID || !got.OracleResult.Passed {
		t.Fatalf("cache result=%+v found=%t err=%v", got, found, err)
	}

	path := filepath.Join(dataStore.Root(), "experiment-cache", key+".json")
	if err := os.WriteFile(path, []byte(`{"key":"`+key+`","complete":false}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, found, err := dataStore.LoadExperimentCache(key); err != nil || found {
		t.Fatalf("incomplete cache reused: found=%t err=%v", found, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("incomplete cache was not invalidated: %v", err)
	}
}
