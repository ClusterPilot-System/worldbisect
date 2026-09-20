// Package hub implements an experimental, summary-only team report service.
// It does not execute checks or independently verify submitted proof labels.
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
	"regexp"
	"time"
)

const MaxBodyBytes = 16 * 1024

type Subject struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Disabled bool   `json:"disabled,omitempty"`
}

type Membership struct {
	SubjectID string `json:"subject_id"`
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
}

type Key struct {
	ID          string     `json:"id,omitempty"`
	SubjectID   string     `json:"subject_id,omitempty"`
	Scopes      []string   `json:"scopes,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	TokenSHA256 string     `json:"token_sha256"`
	Workspace   string     `json:"workspace"`
	Permission  string     `json:"permission"`
}

type Config struct {
	Subjects               []Subject     `json:"subjects,omitempty"`
	Memberships            []Membership  `json:"memberships,omitempty"`
	CIQuarantineUntil      *time.Time    `json:"ci_quarantine_until,omitempty"`
	CIPublishers           []CIPublisher `json:"ci_publishers,omitempty"`
	AuditRetentionDays     int           `json:"audit_retention_days,omitempty"`
	Version                int           `json:"version"`
	RetentionDays          int           `json:"retention_days"`
	MaxReportsPerWorkspace int           `json:"max_reports_per_workspace"`
	Keys                   []Key         `json:"keys"`
}

var workspacePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func (c Config) Validate() error {
	if c.Version != 1 && c.Version != 2 {
		return errors.New("hub configuration version must be 1 or 2")
	}
	if c.RetentionDays < 1 || c.RetentionDays > 30 {
		return errors.New("retention_days must be between 1 and 30")
	}
	if c.MaxReportsPerWorkspace < 1 || c.MaxReportsPerWorkspace > 1000 {
		return errors.New("max_reports_per_workspace must be between 1 and 1000")
	}
	if (len(c.Keys) == 0 && len(c.CIPublishers) == 0) || len(c.Keys) > 100 {
		return errors.New("configuration requires between 1 and 100 keys")
	}
	if err := validateAccessConfig(c); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for i, key := range c.Keys {
		digest, err := hex.DecodeString(key.TokenSHA256)
		if err != nil || len(digest) != sha256.Size || key.TokenSHA256 != hex.EncodeToString(digest) {
			return fmt.Errorf("key %d requires a lowercase SHA-256 token hash", i)
		}
		if seen[key.TokenSHA256] {
			return errors.New("duplicate token hashes are not allowed")
		}
		seen[key.TokenSHA256] = true
		if !workspacePattern.MatchString(key.Workspace) {
			return fmt.Errorf("key %d has an invalid workspace identifier", i)
		}
		if key.Permission != "read" && key.Permission != "write" {
			return fmt.Errorf("key %d permission must be read or write", i)
		}
	}
	return ValidateCIPublishers(c)
}

func LoadConfig(path string) (Config, error) {
	var c Config
	info, err := os.Lstat(path)
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return c, errors.New("hub configuration must be a regular file accessible only to its owner (0600)")
	}
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return c, err
	}
	if len(b) > 64*1024 {
		return c, errors.New("hub configuration exceeds 64 KiB")
	}
	if err := decodeStrict(b, &c); err != nil {
		return c, fmt.Errorf("invalid hub configuration: %w", err)
	}
	return c, c.Validate()
}

// Initialize never overwrites an existing configuration. Raw keys are returned
// exactly once to the operator; only their hashes are persisted.
func Initialize(path string) (map[string]string, error) {
	c := Config{Version: 2, RetentionDays: 7, MaxReportsPerWorkspace: 500, AuditRetentionDays: 7}
	tokens := make(map[string]string)
	for _, permission := range []string{"read", "write"} {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		token := "wbh_" + hex.EncodeToString(random[:])
		digest := sha256.Sum256([]byte(token))
		subject := "default-" + permission
		role := "viewer"
		if permission == "write" {
			role = "editor"
		}
		c.Subjects = append(c.Subjects, Subject{ID: subject, Kind: "service"})
		c.Memberships = append(c.Memberships, Membership{SubjectID: subject, Workspace: "default", Role: role})
		c.Keys = append(c.Keys, Key{ID: subject + "-key", SubjectID: subject, TokenSHA256: hex.EncodeToString(digest[:]), Workspace: "default", Permission: permission})
		tokens[permission] = token
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	ok = true
	return tokens, nil
}
