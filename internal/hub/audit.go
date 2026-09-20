package hub

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuditEvent deliberately excludes token material, IP addresses, submitted
// content, repository identifiers, URLs and query strings.
type AuditEvent struct {
	Actor        string `json:"actor"`
	Kind         string `json:"kind"`
	Workspace    string `json:"workspace,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	Action       string `json:"action"`
	Outcome      string `json:"outcome"`
	ReportID     string `json:"report_id,omitempty"`
}
type auditEntry struct {
	Sequence uint64     `json:"sequence"`
	Time     time.Time  `json:"time"`
	Previous string     `json:"previous"`
	Event    AuditEvent `json:"event"`
	Hash     string     `json:"hash"`
}
type auditSegment struct {
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	LastSequence uint64    `json:"last_sequence"`
	LastHash     string    `json:"last_hash"`
}
type auditState struct {
	Version        int            `json:"version"`
	AnchorSequence uint64         `json:"anchor_sequence"`
	AnchorHash     string         `json:"anchor_hash"`
	HeadSequence   uint64         `json:"head_sequence"`
	HeadHash       string         `json:"head_hash"`
	Segments       []auditSegment `json:"segments"`
}
type AuditStatus struct {
	Sequence         uint64 `json:"sequence"`
	HeadHash         string `json:"head_hash"`
	RetainedSegments int    `json:"retained_segments"`
	AnchorSequence   uint64 `json:"anchor_sequence"`
	AnchorHash       string `json:"anchor_hash"`
}

// AuditLog is a private local hash chain with a committed head checkpoint.
// Its local checkpoint is not an external trust anchor: an administrator who
// can replace the entire directory can rewrite both log and checkpoint.
type AuditLog struct {
	mu           sync.Mutex
	dir          string
	lock         *os.File
	state        auditState
	retention    time.Duration
	segmentBytes int64
	maxSegments  int
	now          func() time.Time
	fault        error
	closed       bool
}

func OpenAudit(dir string, retentionDays int) (*AuditLog, error) {
	if retentionDays == 0 {
		retentionDays = 7
	}
	if retentionDays < 1 || retentionDays > 30 {
		return nil, errors.New("audit retention must be 1..30 days")
	}
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	lock, err := acquireLock(filepath.Join(dir, ".audit.lock"))
	if err != nil {
		return nil, err
	}
	a := &AuditLog{dir: dir, lock: lock, retention: time.Duration(retentionDays) * 24 * time.Hour, segmentBytes: 256 * 1024, maxSegments: 16, now: time.Now}
	statePath := filepath.Join(dir, "state.json")
	if _, err := os.Lstat(statePath); errors.Is(err, os.ErrNotExist) {
		entries, readErr := boundedEntries(dir, 20)
		if readErr != nil {
			lock.Close()
			return nil, readErr
		}
		for _, entry := range entries {
			if entry.Name() != ".audit.lock" {
				lock.Close()
				return nil, errors.New("audit directory has files but no committed checkpoint")
			}
		}
		a.state = auditState{Version: 1, Segments: []auditSegment{}}
		b, _ := json.Marshal(a.state)
		if err := atomicWrite(dir, "state.json", b); err != nil {
			lock.Close()
			return nil, err
		}
	}
	if err := a.loadAndVerify(); err != nil {
		lock.Close()
		return nil, err
	}
	return a, nil
}

func (a *AuditLog) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	return a.lock.Close()
}
func (a *AuditLog) SetRetention(days int) {
	if a == nil {
		return
	}
	if days == 0 {
		days = 7
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retention = time.Duration(days) * 24 * time.Hour
}
func (a *AuditLog) Status() AuditStatus {
	if a == nil {
		return AuditStatus{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return AuditStatus{a.state.HeadSequence, a.state.HeadHash, len(a.state.Segments), a.state.AnchorSequence, a.state.AnchorHash}
}

func (a *AuditLog) Append(event AuditEvent) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("audit log is closed")
	}
	if a.fault != nil {
		return a.fault
	}
	if err := validateAuditEvent(event); err != nil {
		return err
	}
	entry := auditEntry{Sequence: a.state.HeadSequence + 1, Time: a.now().UTC(), Previous: a.state.HeadHash, Event: event}
	entry.Hash = hashAuditEntry(entry)
	b, _ := json.Marshal(entry)
	b = append(b, '\n')
	if len(b) > 4096 {
		return errors.New("audit event exceeds size limit")
	}
	next := a.state
	next.Segments = append([]auditSegment{}, a.state.Segments...)
	newSegment := len(next.Segments) == 0
	if !newSegment {
		info, err := os.Lstat(filepath.Join(a.dir, next.Segments[len(next.Segments)-1].Name))
		if err != nil {
			return a.fail(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return a.fail(errors.New("unsafe audit segment"))
		}
		newSegment = info.Size()+int64(len(b)) > a.segmentBytes || !next.Segments[len(next.Segments)-1].CreatedAt.After(a.now().UTC().Add(-a.retention))
	}
	if newSegment {
		next.Segments = append(next.Segments, auditSegment{Name: fmt.Sprintf("%020d.jsonl", entry.Sequence), CreatedAt: entry.Time})
	}
	current := &next.Segments[len(next.Segments)-1]
	flags := os.O_WRONLY | os.O_APPEND
	if newSegment {
		flags |= os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(filepath.Join(a.dir, current.Name), flags, 0600)
	if err != nil {
		return a.fail(err)
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return a.fail(err)
	}
	current.LastSequence = entry.Sequence
	current.LastHash = entry.Hash
	next.HeadSequence = entry.Sequence
	next.HeadHash = entry.Hash
	next, removed := a.pruned(next)
	if err := a.commit(next, removed); err != nil {
		return a.fail(err)
	}
	return nil
}

func (a *AuditLog) fail(err error) error {
	a.fault = fmt.Errorf("audit integrity/storage failure: %w", err)
	return a.fault
}
func (a *AuditLog) pruned(state auditState) (auditState, []string) {
	var removed []string
	cutoff := a.now().UTC().Add(-a.retention)
	for len(state.Segments) > 0 && (len(state.Segments) > a.maxSegments || !state.Segments[0].CreatedAt.After(cutoff)) {
		segment := state.Segments[0]
		removed = append(removed, segment.Name)
		state.AnchorSequence = segment.LastSequence
		state.AnchorHash = segment.LastHash
		state.Segments = state.Segments[1:]
	}
	return state, removed
}
func (a *AuditLog) commit(state auditState, removed []string) error {
	b, _ := json.Marshal(state)
	if err := atomicWrite(a.dir, "state.json", b); err != nil {
		return err
	}
	a.state = state
	for _, name := range removed {
		if err := os.Remove(filepath.Join(a.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return syncDirectory(a.dir)
}
func (a *AuditLog) PurgeExpired() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("audit log is closed")
	}
	if a.fault != nil {
		return a.fault
	}
	state := a.state
	state.Segments = append([]auditSegment{}, state.Segments...)
	next, removed := a.pruned(state)
	if len(removed) == 0 {
		return nil
	}
	if err := a.commit(next, removed); err != nil {
		return a.fail(err)
	}
	return nil
}

func validateAuditEvent(e AuditEvent) error {
	for _, value := range []string{e.Actor, e.Kind, e.Workspace, e.CredentialID, e.Action, e.Outcome, e.ReportID} {
		if len(value) > 128 || strings.ContainsAny(value, "\n\r\x00") {
			return errors.New("invalid audit metadata")
		}
	}
	if e.Actor == "" || e.Action == "" || e.Outcome == "" {
		return errors.New("audit actor, action and outcome are required")
	}
	return nil
}
func hashAuditEntry(entry auditEntry) string {
	entry.Hash = ""
	b, _ := json.Marshal(entry)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}
func auditFile(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > max {
		return nil, errors.New("unsafe or oversized audit file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(b)) > max {
		return nil, errors.New("oversized audit file")
	}
	return b, err
}
func (a *AuditLog) loadAndVerify() error {
	b, err := auditFile(filepath.Join(a.dir, "state.json"), 65536)
	if err != nil {
		return err
	}
	var state auditState
	if err := decodeStrict(b, &state); err != nil {
		return err
	}
	if state.Version != 1 || len(state.Segments) > 16 || state.AnchorSequence > state.HeadSequence {
		return errors.New("invalid audit checkpoint")
	}
	sequence, previous := state.AnchorSequence, state.AnchorHash
	validHash := func(seq uint64, hash string) bool {
		if seq == 0 {
			return hash == ""
		}
		raw, err := hex.DecodeString(hash)
		return err == nil && len(raw) == 32 && hash == hex.EncodeToString(raw)
	}
	if !validHash(sequence, previous) || !validHash(state.HeadSequence, state.HeadHash) {
		return errors.New("invalid audit checkpoint digest")
	}
	keep := map[string]bool{"state.json": true, ".audit.lock": true}
	for _, segment := range state.Segments {
		if segment.Name != fmt.Sprintf("%020d.jsonl", sequence+1) || keep[segment.Name] || segment.CreatedAt.IsZero() {
			return errors.New("invalid audit segment order")
		}
		keep[segment.Name] = true
		b, err := auditFile(filepath.Join(a.dir, segment.Name), 256*1024)
		if err != nil {
			return err
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			return errors.New("truncated audit segment")
		}
		scanner := bufio.NewScanner(bytes.NewReader(b))
		scanner.Buffer(make([]byte, 4096), 4096)
		for scanner.Scan() {
			var entry auditEntry
			if err := decodeStrict(scanner.Bytes(), &entry); err != nil {
				return err
			}
			if entry.Sequence != sequence+1 || entry.Previous != previous || entry.Time.IsZero() || entry.Hash != hashAuditEntry(entry) {
				return errors.New("audit hash chain mismatch")
			}
			if err := validateAuditEvent(entry.Event); err != nil {
				return err
			}
			sequence = entry.Sequence
			previous = entry.Hash
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if sequence != segment.LastSequence || previous != segment.LastHash {
			return errors.New("audit segment disagrees with committed checkpoint")
		}
	}
	if sequence != state.HeadSequence || previous != state.HeadHash {
		return errors.New("audit head disagrees with committed checkpoint")
	}
	entries, err := boundedEntries(a.dir, 40)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if keep[entry.Name()] {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".tmp-") {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("unsafe audit temporary file")
			}
			if err := os.Remove(filepath.Join(a.dir, name)); err != nil {
				return err
			}
			continue
		}
		var start uint64
		if _, err := fmt.Sscanf(name, "%020d.jsonl", &start); err != nil || name != fmt.Sprintf("%020d.jsonl", start) || start > state.AnchorSequence {
			return errors.New("unexpected or uncommitted audit file")
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("unsafe pruned audit file")
		}
		if err := os.Remove(filepath.Join(a.dir, name)); err != nil {
			return err
		}
	}
	a.state = state
	return nil
}

// VerifyAudit requires an existing stopped audit store, checks the complete
// retained chain and committed head, and returns its checkpoint for export.
func VerifyAudit(dir, expectedHead string) (AuditStatus, error) {
	if _, err := os.Lstat(filepath.Join(dir, "state.json")); err != nil {
		return AuditStatus{}, err
	}
	a, err := OpenAudit(dir, 7)
	if err != nil {
		return AuditStatus{}, err
	}
	defer a.Close()
	status := a.Status()
	if expectedHead != "" && status.HeadHash != expectedHead {
		return status, errors.New("audit head does not match external expected checkpoint")
	}
	return status, nil
}
