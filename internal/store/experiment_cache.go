package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

// MaxExperimentCacheBytes bounds persisted experiment evidence. Cache data is
// disposable; exceeding the limit must never make an analysis fail.
const MaxExperimentCacheBytes int64 = 64 << 20

type experimentCacheEntry struct {
	Key        string           `json:"key"`
	Complete   bool             `json:"complete"`
	Experiment model.Experiment `json:"experiment"`
}

// LoadExperimentCache returns only a complete entry for the exact key. Invalid
// or incomplete entries are discarded as stale cache state, never interpreted
// as experiment evidence.
func (store *Store) LoadExperimentCache(key string) (model.Experiment, bool, error) {
	if !validDigest(key) {
		return model.Experiment{}, false, errors.New("invalid experiment cache key")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	path := filepath.Join(store.root, "experiment-cache", key+".json")
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.Experiment{}, false, nil
	}
	if err != nil {
		return model.Experiment{}, false, err
	}
	var entry experimentCacheEntry
	if err := json.Unmarshal(content, &entry); err != nil || entry.Key != key || !entry.Complete {
		_ = os.Remove(path)
		return model.Experiment{}, false, nil
	}
	return entry.Experiment, true, nil
}

// SaveExperimentCache atomically persists one complete observed experiment and
// prunes deterministically until the bounded cache fits. Cache failures are
// intentionally returned to the caller so analysis can ignore them safely.
func (store *Store) SaveExperimentCache(key string, experiment model.Experiment) error {
	if !validDigest(key) {
		return errors.New("invalid experiment cache key")
	}
	entry := experimentCacheEntry{Key: key, Complete: true, Experiment: experiment}
	content, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	if int64(len(content)) > MaxExperimentCacheBytes {
		return errors.New("experiment cache entry exceeds size limit")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	path := filepath.Join(store.root, "experiment-cache", key+".json")
	if err := writeFileAtomic(path, content, 0o640); err != nil {
		return err
	}
	return store.pruneExperimentCacheLocked(key)
}

func (store *Store) pruneExperimentCacheLocked(preserve string) error {
	paths, err := filepath.Glob(filepath.Join(store.root, "experiment-cache", "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	var total int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		total += info.Size()
	}
	for _, path := range paths {
		if total <= MaxExperimentCacheBytes || filepath.Base(path) == preserve+".json" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		total -= info.Size()
	}
	if total > MaxExperimentCacheBytes {
		return os.Remove(filepath.Join(store.root, "experiment-cache", preserve+".json"))
	}
	return nil
}
