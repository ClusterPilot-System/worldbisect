package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func (store *Store) AppendAudit(action, entityType, entityID string, details map[string]any) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.appendAuditLocked("local", action, entityType, entityID, "", details)
}

func (store *Store) appendAuditLocked(actor, action, entityType, entityID, requestID string, details map[string]any) error {
	path := filepath.Join(store.root, "audit", "events.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	previous, sequence, err := lastAudit(path)
	if err != nil {
		return err
	}
	event := model.AuditEvent{
		Version:      1,
		Sequence:     sequence + 1,
		Timestamp:    time.Now().UTC(),
		Actor:        actor,
		Action:       action,
		EntityType:   entityType,
		EntityID:     entityID,
		RequestID:    requestID,
		Details:      details,
		PreviousHash: previous,
	}
	event.Hash, err = auditHash(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(event); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (store *Store) VerifyAudit() (model.AuditVerification, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	path := filepath.Join(store.root, "audit", "events.jsonl")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.AuditVerification{Valid: true}, nil
	}
	if err != nil {
		return model.AuditVerification{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	previous := ""
	var sequence uint64
	for scanner.Scan() {
		var event model.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return model.AuditVerification{Valid: false, Entries: sequence, Error: err.Error()}, nil
		}
		if event.Sequence != sequence+1 || event.PreviousHash != previous {
			return model.AuditVerification{Valid: false, Entries: sequence, Error: "sequence or previous hash mismatch"}, nil
		}
		expected, err := auditHash(event)
		if err != nil {
			return model.AuditVerification{}, err
		}
		if expected != event.Hash {
			return model.AuditVerification{Valid: false, Entries: sequence, Error: "event hash mismatch"}, nil
		}
		previous = event.Hash
		sequence = event.Sequence
	}
	if err := scanner.Err(); err != nil {
		return model.AuditVerification{}, err
	}
	return model.AuditVerification{Valid: true, Entries: sequence}, nil
}

func lastAudit(path string) (string, uint64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var last model.AuditEvent
	for scanner.Scan() {
		if err := json.Unmarshal(scanner.Bytes(), &last); err != nil {
			return "", 0, err
		}
	}
	if err := scanner.Err(); err != nil {
		return "", 0, err
	}
	return last.Hash, last.Sequence, nil
}

func auditHash(event model.AuditEvent) (string, error) {
	event.Hash = ""
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

var _ = fmt.Sprintf
