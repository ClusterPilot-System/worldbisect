package hub

import (
	"crypto/rand"
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
)

var ErrNotFound = errors.New("report not found")
var ErrQuota = errors.New("workspace report quota reached; delete an older report or wait for retention expiry")

// Store is owned by a single process. The OS lock also prevents a second hub
// instance from exceeding quotas or racing atomic report transactions.
type Store struct {
	mu     sync.Mutex
	root   string
	config Config
	lock   *os.File
	now    func() time.Time
	closed bool
}

func OpenStore(root string, config Config) (*Store, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := privateDir(root); err != nil {
		return nil, err
	}
	lock, err := acquireLock(filepath.Join(root, ".hub.lock"))
	if err != nil {
		return nil, fmt.Errorf("lock hub data directory: %w", err)
	}
	s := &Store{root: root, config: config, lock: lock, now: time.Now}
	if err := s.PurgeExpired(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("hub data directories must be real directories accessible only to their owner (0700)")
	}
	return nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.lock.Close()
}

func (s *Store) workspaceDir(workspace string) (string, error) {
	if s.closed {
		return "", errors.New("hub store is closed")
	}
	allowed := false
	for _, key := range s.config.Keys {
		if key.Workspace == workspace {
			allowed = true
			break
		}
	}
	for _, member := range s.config.Memberships {
		if member.Workspace == workspace {
			allowed = true
		}
	}
	if !allowed {
		return "", ErrNotFound
	}
	digest := sha256.Sum256([]byte(workspace))
	dir := filepath.Join(s.root, hex.EncodeToString(digest[:]))
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		entries, err := boundedEntries(s.root, 116)
		if err != nil {
			return "", err
		}
		count := 0
		for _, entry := range entries {
			if entry.IsDir() {
				count++
			}
		}
		if count >= 100 {
			return "", errors.New("hub workspace directory limit reached")
		}
	}
	return dir, privateDir(dir)
}

func boundedEntries(dir string, limit int) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(limit + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > limit {
		return nil, errors.New("hub storage directory exceeds its entry limit")
	}
	return entries, nil
}

// load prunes expired records before returning. Reads are individually bounded
// and fail closed for malformed records, symlinks or unexpected store entries.
func (s *Store) load(dir string) ([]Report, error) {
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	entries, err := boundedEntries(dir, 1016)
	if err != nil {
		return nil, err
	}
	reports := make([]Report, 0, len(entries))
	cutoff := s.now().UTC().Add(-time.Duration(s.config.RetentionDays) * 24 * time.Hour)
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("invalid hub report file type or permissions")
		}
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !strings.HasSuffix(entry.Name(), ".json") || !idPattern.MatchString(id) {
			return nil, errors.New("unexpected hub report filename")
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(f, 65537))
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(b) > 65536 {
			return nil, errors.New("stored hub report exceeds 64 KiB")
		}
		var report Report
		if err := decodeStrict(b, &report); err != nil {
			return nil, errors.New("invalid stored hub report")
		}
		if report.ID != id || report.CreatedAt.IsZero() || report.CreatedAt.After(s.now().UTC().Add(time.Minute)) {
			return nil, errors.New("invalid stored hub report identity or timestamp")
		}
		if err := report.Submission.Validate(); err != nil {
			return nil, fmt.Errorf("invalid stored hub report: %w", err)
		}
		if err := report.Publisher.validate(report.Submission); err != nil {
			return nil, err
		}
		if !report.CreatedAt.After(cutoff) {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			continue
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (s *Store) List(workspace string) ([]Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.workspaceDir(workspace)
	if err != nil {
		return nil, err
	}
	reports, err := s.load(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].CreatedAt.Equal(reports[j].CreatedAt) {
			return reports[i].ID > reports[j].ID
		}
		return reports[i].CreatedAt.After(reports[j].CreatedAt)
	})
	if len(reports) > 100 {
		reports = reports[:100]
	}
	return reports, nil
}

func (s *Store) Create(workspace string, submission Submission) (Report, error) {
	return s.CreateVerified(workspace, submission, nil)
}

// CreateVerified accepts origin metadata only from the internal CI verifier.
// Submission never contains this field, preventing spoofing through JSON input.
func (s *Store) CreateVerified(workspace string, submission Submission, publisher *PublisherIdentity) (Report, error) {
	if err := submission.Validate(); err != nil {
		return Report{}, err
	}
	if err := publisher.validate(submission); err != nil {
		return Report{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.workspaceDir(workspace)
	if err != nil {
		return Report{}, err
	}
	reports, err := s.load(dir)
	if err != nil {
		return Report{}, err
	}
	if len(reports) >= s.config.MaxReportsPerWorkspace {
		return Report{}, ErrQuota
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Report{}, err
	}
	report := Report{ID: hex.EncodeToString(random[:]), CreatedAt: s.now().UTC(), Submission: submission, Publisher: publisher}
	b, err := json.Marshal(report)
	if err != nil {
		return Report{}, err
	}
	if len(b) > 65536 {
		return Report{}, errors.New("encoded report is too large")
	}
	if err := atomicWrite(dir, report.ID+".json", b); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (s *Store) Get(workspace, id string) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.workspaceDir(workspace)
	if err != nil {
		return Report{}, err
	}
	reports, err := s.load(dir)
	if err != nil {
		return Report{}, err
	}
	for _, report := range reports {
		if report.ID == id {
			return report, nil
		}
	}
	return Report{}, ErrNotFound
}

func (s *Store) Delete(workspace, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.workspaceDir(workspace)
	if err != nil {
		return err
	}
	reports, err := s.load(dir)
	if err != nil {
		return err
	}
	for _, report := range reports {
		if report.ID == id {
			if err := os.Remove(filepath.Join(dir, id+".json")); err != nil {
				return err
			}
			return syncDirectory(dir)
		}
	}
	return ErrNotFound
}

// PurgeExpired also covers workspaces whose last token was removed. Callers
// should run it periodically even when the hub receives no traffic.
func (s *Store) PurgeExpired() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("hub store is closed")
	}
	entries, err := boundedEntries(s.root, 116)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".hub.lock" {
			continue
		}
		if entry.Name() == replayFilename {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256*1024 {
				return errors.New("invalid CI replay ledger")
			}
			continue
		}
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			// Only private regular files from interrupted atomic ledger writes.
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return errors.New("invalid temporary hub file")
			}
			if err := os.Remove(filepath.Join(s.root, entry.Name())); err != nil {
				return err
			}
			continue
		}
		digest, err := hex.DecodeString(entry.Name())
		if err != nil || len(digest) != sha256.Size || entry.Name() != hex.EncodeToString(digest) || !entry.IsDir() {
			return errors.New("unexpected entry in hub data directory")
		}
		dir := filepath.Join(s.root, entry.Name())
		reports, err := s.load(dir)
		if err != nil {
			return err
		}
		if len(reports) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

func atomicWrite(dir, name string, b []byte) error {
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
