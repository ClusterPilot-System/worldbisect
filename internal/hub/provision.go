package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

type CredentialOptions struct {
	SubjectID string
	Kind      string
	Workspace string
	Role      string
	ExpiresIn time.Duration
}

// ProvisionCredential adds an explicitly named operator-provisioned identity
// and membership, then writes a private config atomically. A running server
// must receive SIGHUP before the new credential becomes valid.
func ProvisionCredential(path string, options CredentialOptions) (Key, string, error) {
	lock, err := acquireLock(path + ".lock")
	if err != nil {
		return Key{}, "", err
	}
	defer lock.Close()
	c, err := LoadConfig(path)
	if err != nil {
		return Key{}, "", err
	}
	if c.Version != 2 {
		return Key{}, "", errors.New("credential provisioning requires config version 2; migrate named subjects/memberships first")
	}
	if !workspacePattern.MatchString(options.SubjectID) || !workspacePattern.MatchString(options.Workspace) || (options.Kind != "user" && options.Kind != "service") || len(roleScopes(options.Role)) == 0 {
		return Key{}, "", errors.New("provide valid subject, kind user/service, workspace and role viewer/editor/publisher")
	}
	if options.ExpiresIn < 0 || options.ExpiresIn > 365*24*time.Hour {
		return Key{}, "", errors.New("credential expiry duration must be zero (no expiry) or at most 365 days")
	}
	found := false
	for _, subject := range c.Subjects {
		if subject.ID == options.SubjectID {
			if subject.Kind != options.Kind || subject.Disabled {
				return Key{}, "", errors.New("existing subject kind differs or subject is disabled")
			}
			found = true
		}
	}
	if !found {
		c.Subjects = append(c.Subjects, Subject{ID: options.SubjectID, Kind: options.Kind})
	}
	found = false
	for _, member := range c.Memberships {
		if member.SubjectID == options.SubjectID && member.Workspace == options.Workspace {
			if member.Role != options.Role {
				return Key{}, "", errors.New("existing membership role differs; review and edit the membership explicitly")
			}
			found = true
		}
	}
	if !found {
		c.Memberships = append(c.Memberships, Membership{SubjectID: options.SubjectID, Workspace: options.Workspace, Role: options.Role})
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Key{}, "", err
	}
	token := "wbh_" + hex.EncodeToString(random[:])
	digest := sha256.Sum256([]byte(token))
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Key{}, "", err
	}
	permission := "write"
	if options.Role == "viewer" {
		permission = "read"
	}
	key := Key{ID: "key-" + hex.EncodeToString(id[:]), SubjectID: options.SubjectID, Workspace: options.Workspace, Permission: permission, Scopes: roleScopes(options.Role), TokenSHA256: hex.EncodeToString(digest[:])}
	if options.ExpiresIn > 0 {
		expiry := time.Now().UTC().Add(options.ExpiresIn)
		key.ExpiresAt = &expiry
	}
	c.Keys = append(c.Keys, key)
	if err := c.Validate(); err != nil {
		return Key{}, "", err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return Key{}, "", err
	}
	if len(b)+1 > 64*1024 {
		return Key{}, "", errors.New("updated configuration would exceed 64 KiB")
	}
	if err := atomicWrite(filepath.Dir(path), filepath.Base(path), append(b, '\n')); err != nil {
		return Key{}, "", err
	}
	return key, token, nil
}
