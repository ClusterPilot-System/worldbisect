package store

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

const currentSchemaVersion = 3

var ErrIdempotencyConflict = errors.New("idempotency conflict")

type Store struct {
	root  string
	mutex sync.Mutex
}

type schemaFile struct {
	Version int `json:"version"`
}

type CreateJobRequest struct {
	Job                  model.Job
	PrincipalFingerprint string
	Route                string
	IdempotencyKey       string
	RequestDigest        string
}

func Open(root string) (*Store, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	store := &Store{root: absolute}
	if err := store.initialize(); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *Store) Root() string { return store.root }

func (store *Store) initialize() error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	for _, directory := range []string{"captures", "analyses", "jobs", "idempotency", "experiment-cache", "blobs/sha256", "audit", "keys", "traces"} {
		if err := os.MkdirAll(filepath.Join(store.root, directory), 0o750); err != nil {
			return err
		}
	}
	path := filepath.Join(store.root, "schema.json")
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeJSONAtomic(path, schemaFile{Version: currentSchemaVersion}, 0o640)
	}
	if err != nil {
		return err
	}
	var schema schemaFile
	if err := json.Unmarshal(content, &schema); err != nil {
		return err
	}
	if schema.Version > currentSchemaVersion {
		return fmt.Errorf("store schema %d is newer than supported %d", schema.Version, currentSchemaVersion)
	}
	for schema.Version < currentSchemaVersion {
		switch schema.Version {
		case 1:
			if err := store.migrateV1ToV2(); err != nil {
				return err
			}
			schema.Version = 2
		case 2:
			if err := store.migrateV2ToV3(); err != nil {
				return err
			}
			schema.Version = 3
		default:
			return fmt.Errorf("no migration from schema %d", schema.Version)
		}
		if err := writeJSONAtomic(path, schema, 0o640); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) migrateV1ToV2() error {
	return os.MkdirAll(filepath.Join(store.root, "analyses"), 0o750)
}

func (store *Store) migrateV2ToV3() error {
	for _, directory := range []string{"jobs", "idempotency", "traces"} {
		if err := os.MkdirAll(filepath.Join(store.root, directory), 0o750); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) SaveCapture(value *model.Capture) error {
	value.SchemaVersion = currentSchemaVersion
	value.Normalize()
	return store.saveEntity("captures", value.ID, value)
}

func (store *Store) LoadCapture(identifier string) (*model.Capture, error) {
	var value model.Capture
	if err := store.loadEntity("captures", identifier, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (store *Store) ListCaptures(limit int) ([]model.Capture, error) {
	var values []model.Capture
	if err := store.listEntities("captures", limit, func(content []byte) error {
		var value model.Capture
		if err := json.Unmarshal(content, &value); err != nil {
			return err
		}
		values = append(values, value)
		return nil
	}); err != nil {
		return nil, err
	}
	return values, nil
}

func (store *Store) SaveAnalysis(value *model.Analysis) error {
	value.SchemaVersion = currentSchemaVersion
	value.Normalize()
	return store.saveEntity("analyses", value.ID, value)
}

func (store *Store) LoadAnalysis(identifier string) (*model.Analysis, error) {
	var value model.Analysis
	if err := store.loadEntity("analyses", identifier, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (store *Store) ListAnalyses(limit int) ([]model.Analysis, error) {
	var values []model.Analysis
	if err := store.listEntities("analyses", limit, func(content []byte) error {
		var value model.Analysis
		if err := json.Unmarshal(content, &value); err != nil {
			return err
		}
		values = append(values, value)
		return nil
	}); err != nil {
		return nil, err
	}
	return values, nil
}

func (store *Store) SaveJob(value *model.Job) error {
	value.SchemaVersion = currentSchemaVersion
	return store.saveEntity("jobs", value.ID, value)
}

func (store *Store) LoadJob(identifier string) (*model.Job, error) {
	var value model.Job
	if err := store.loadEntity("jobs", identifier, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (store *Store) ListJobs(limit int) ([]model.Job, error) {
	var values []model.Job
	if err := store.listEntities("jobs", limit, func(content []byte) error {
		var value model.Job
		if err := json.Unmarshal(content, &value); err != nil {
			return err
		}
		values = append(values, value)
		return nil
	}); err != nil {
		return nil, err
	}
	return values, nil
}

func (store *Store) CreateJobIdempotent(request CreateJobRequest) (*model.Job, bool, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if request.IdempotencyKey == "" || request.RequestDigest == "" {
		return nil, false, errors.New("idempotency key and request digest are required")
	}
	idempotencyID := digestString(request.PrincipalFingerprint + "\x00" + request.Route + "\x00" + request.IdempotencyKey)
	idempotencyPath := filepath.Join(store.root, "idempotency", idempotencyID+".json")
	if content, err := os.ReadFile(idempotencyPath); err == nil {
		var existing model.IdempotencyRecord
		if err := json.Unmarshal(content, &existing); err != nil {
			return nil, false, err
		}
		if existing.RequestDigest != request.RequestDigest {
			return nil, false, ErrIdempotencyConflict
		}
		var job model.Job
		if err := readJSON(filepath.Join(store.root, "jobs", existing.JobID+".json"), &job); err != nil {
			return nil, false, err
		}
		return &job, true, nil
	}
	request.Job.SchemaVersion = currentSchemaVersion
	request.Job.CreatedAt = time.Now().UTC()
	request.Job.UpdatedAt = request.Job.CreatedAt
	if err := writeJSONAtomic(filepath.Join(store.root, "jobs", request.Job.ID+".json"), request.Job, 0o640); err != nil {
		return nil, false, err
	}
	record := model.IdempotencyRecord{
		SchemaVersion:        currentSchemaVersion,
		PrincipalFingerprint: request.PrincipalFingerprint,
		Route:                request.Route,
		Key:                  request.IdempotencyKey,
		RequestDigest:        request.RequestDigest,
		JobID:                request.Job.ID,
		CreatedAt:            time.Now().UTC(),
	}
	if err := writeJSONAtomic(idempotencyPath, record, 0o640); err != nil {
		_ = os.Remove(filepath.Join(store.root, "jobs", request.Job.ID+".json"))
		return nil, false, err
	}
	if err := store.appendAuditLocked(request.PrincipalFingerprint, "job-enqueued", "job", request.Job.ID, "", map[string]any{"type": request.Job.Type}); err != nil {
		return nil, false, err
	}
	copy := request.Job
	return &copy, false, nil
}

func (store *Store) ClaimNextJob(workerID string, lease time.Duration, maxAttempts int) (*model.Job, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	files, err := filepath.Glob(filepath.Join(store.root, "jobs", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	now := time.Now().UTC()
	for _, path := range files {
		var job model.Job
		if err := readJSON(path, &job); err != nil {
			return nil, err
		}
		if job.State != model.JobQueued || job.Attempts >= maxAttempts {
			continue
		}
		job.State = model.JobRunning
		job.LeaseOwner = workerID
		job.LeaseExpires = now.Add(lease)
		job.HeartbeatAt = now
		job.Attempts++
		job.UpdatedAt = now
		if err := writeJSONAtomic(path, job, 0o640); err != nil {
			return nil, err
		}
		if err := store.appendAuditLocked(workerID, "job-claimed", "job", job.ID, "", map[string]any{"attempt": job.Attempts}); err != nil {
			return nil, err
		}
		return &job, nil
	}
	return nil, nil
}

func (store *Store) HeartbeatJob(jobID, workerID string, lease time.Duration) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	path := filepath.Join(store.root, "jobs", jobID+".json")
	var job model.Job
	if err := readJSON(path, &job); err != nil {
		return err
	}
	if job.State != model.JobRunning || job.LeaseOwner != workerID {
		return errors.New("job lease is not owned by worker")
	}
	now := time.Now().UTC()
	job.HeartbeatAt = now
	job.LeaseExpires = now.Add(lease)
	job.UpdatedAt = now
	return writeJSONAtomic(path, job, 0o640)
}

func (store *Store) CompleteJob(jobID, workerID string, state model.JobState, resultRef, errorMessage string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	path := filepath.Join(store.root, "jobs", jobID+".json")
	var job model.Job
	if err := readJSON(path, &job); err != nil {
		return err
	}
	if job.State != model.JobRunning || job.LeaseOwner != workerID {
		return errors.New("job lease is not owned by worker")
	}
	if state != model.JobSucceeded && state != model.JobFailed {
		return errors.New("invalid terminal job state")
	}
	job.State = state
	job.ResultRef = resultRef
	job.Error = errorMessage
	job.LeaseOwner = ""
	job.LeaseExpires = time.Time{}
	job.UpdatedAt = time.Now().UTC()
	if err := writeJSONAtomic(path, job, 0o640); err != nil {
		return err
	}
	return store.appendAuditLocked(workerID, "job-completed", "job", job.ID, "", map[string]any{"state": state})
}

func (store *Store) RequeueExpiredJobs(maxAttempts int) (int, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	files, err := filepath.Glob(filepath.Join(store.root, "jobs", "*.json"))
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	count := 0
	for _, path := range files {
		var job model.Job
		if err := readJSON(path, &job); err != nil {
			return count, err
		}
		if job.State != model.JobRunning || job.LeaseExpires.IsZero() || job.LeaseExpires.After(now) {
			continue
		}
		if job.Attempts >= maxAttempts {
			job.State = model.JobFailed
			job.Error = "job lease expired and maximum attempts were reached"
		} else {
			job.State = model.JobQueued
		}
		job.LeaseOwner = ""
		job.LeaseExpires = time.Time{}
		job.UpdatedAt = now
		if err := writeJSONAtomic(path, job, 0o640); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (store *Store) PutBlob(content []byte) (string, error) {
	digest := digestBytes(content)
	path := store.blobPath(digest)
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if _, err := os.Stat(path); err == nil {
		return digest, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	buffer := &bytes.Buffer{}
	writer, err := gzip.NewWriterLevel(buffer, gzip.BestSpeed)
	if err != nil {
		return "", err
	}
	writer.Header.ModTime = time.Unix(0, 0).UTC()
	writer.Header.OS = 255
	if _, err := writer.Write(content); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, buffer.Bytes(), 0o640); err != nil {
		return "", err
	}
	return digest, nil
}

func (store *Store) GetBlob(digest string) ([]byte, error) {
	if !validDigest(digest) {
		return nil, errors.New("invalid blob digest")
	}
	file, err := os.Open(store.blobPath(digest))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, 1<<30))
	if err != nil {
		return nil, err
	}
	if digestBytes(content) != digest {
		return nil, errors.New("blob digest mismatch")
	}
	return content, nil
}

func (store *Store) AppendTrace(span model.TraceSpan) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	path := filepath.Join(store.root, "traces", "spans.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(span); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (store *Store) saveEntity(directory, identifier string, value any) error {
	if !validIdentifier(identifier) {
		return errors.New("invalid entity identifier")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return writeJSONAtomic(filepath.Join(store.root, directory, identifier+".json"), value, 0o640)
}

func (store *Store) loadEntity(directory, identifier string, value any) error {
	if !validIdentifier(identifier) {
		return errors.New("invalid entity identifier")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return readJSON(filepath.Join(store.root, directory, identifier+".json"), value)
}

func (store *Store) listEntities(directory string, limit int, decode func([]byte) error) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	files, err := filepath.Glob(filepath.Join(store.root, directory, "*.json"))
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := decode(content); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) blobPath(digest string) string {
	return filepath.Join(store.root, "blobs", "sha256", digest[:2], digest+".json.gz")
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeFileAtomic(path, content, mode)
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".worldbisect-store-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
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
	return os.Rename(name, path)
}

func readJSON(path string, value any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func digestString(value string) string { return digestBytes([]byte(value)) }

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return !strings.Contains(value, "..")
}
