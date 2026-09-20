package hub

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Principal is an operator-provisioned identity; it does not assert that an
// external identity provider or a human login has been verified.
type Principal struct {
	SubjectID    string   `json:"subject_id"`
	Kind         string   `json:"kind"`
	Workspace    string   `json:"workspace"`
	CredentialID string   `json:"credential_id"`
	Scopes       []string `json:"scopes"`
}

func validateAccessConfig(c Config) error {
	if c.CIQuarantineUntil != nil && (c.Version != 2 || c.CIQuarantineUntil.IsZero()) {
		return errors.New("ci_quarantine_until requires version 2 and a valid timestamp")
	}
	if c.AuditRetentionDays < 0 || c.AuditRetentionDays > 30 {
		return errors.New("audit_retention_days must be 1..30 or omitted for 7 days")
	}
	if len(c.Subjects) > 100 || len(c.Memberships) > 200 {
		return errors.New("access configuration exceeds 100 subjects or 200 memberships")
	}
	if c.Version == 1 && (len(c.Subjects) > 0 || len(c.Memberships) > 0 || len(c.CIPublishers) > 0) {
		return errors.New("subjects, memberships and CI publishers require configuration version 2")
	}
	subjects := map[string]bool{}
	for _, subject := range c.Subjects {
		if !workspacePattern.MatchString(subject.ID) || (subject.Kind != "user" && subject.Kind != "service") || subjects[subject.ID] {
			return errors.New("subjects require unique identifiers and kind user or service")
		}
		subjects[subject.ID] = true
	}
	memberships := map[string]bool{}
	for _, member := range c.Memberships {
		key := member.SubjectID + "/" + member.Workspace
		if !subjects[member.SubjectID] || !workspacePattern.MatchString(member.Workspace) || memberships[key] || len(roleScopes(member.Role)) == 0 {
			return errors.New("membership requires a configured subject, workspace and unique viewer/editor/publisher role")
		}
		memberships[key] = true
	}
	ids := map[string]bool{}
	for _, key := range c.Keys {
		if c.Version == 1 {
			if key.ID != "" || key.SubjectID != "" || len(key.Scopes) > 0 {
				return errors.New("credential identities and scopes require configuration version 2")
			}
			continue
		}
		if !workspacePattern.MatchString(key.ID) || ids[key.ID] || !subjects[key.SubjectID] {
			return errors.New("v2 credentials require unique id and configured subject_id")
		}
		ids[key.ID] = true
		if !memberships[key.SubjectID+"/"+key.Workspace] {
			return fmt.Errorf("credential %s has no workspace membership", key.ID)
		}
		if len(key.Scopes) > 3 {
			return errors.New("a credential accepts at most three report scopes")
		}
		seen := map[string]bool{}
		for _, scope := range key.Scopes {
			if !slices.Contains([]string{"reports:read", "reports:write", "reports:delete"}, scope) || seen[scope] {
				return errors.New("credential scopes must be unique reports:read, reports:write or reports:delete")
			}
			seen[scope] = true
		}
		for _, scope := range keyScopes(key) {
			if !slices.Contains(membershipScopes(c, key.SubjectID, key.Workspace), scope) {
				return fmt.Errorf("credential %s exceeds its membership role", key.ID)
			}
		}
	}
	return nil
}

func roleScopes(role string) []string {
	switch role {
	case "viewer":
		return []string{"reports:read"}
	case "publisher":
		return []string{"reports:write"}
	case "editor":
		return []string{"reports:read", "reports:write", "reports:delete"}
	}
	return nil
}

func membershipScopes(c Config, subject, workspace string) []string {
	for _, member := range c.Memberships {
		if member.SubjectID == subject && member.Workspace == workspace {
			return roleScopes(member.Role)
		}
	}
	return nil
}

func keyScopes(key Key) []string {
	if len(key.Scopes) > 0 {
		return key.Scopes
	}
	if key.Permission == "write" {
		return roleScopes("editor")
	}
	return roleScopes("viewer")
}

func principalForKey(c Config, key Key, now time.Time) (Principal, bool) {
	if key.TokenSHA256 == "" {
		return Principal{}, false
	}
	if key.ExpiresAt != nil && !now.Before(*key.ExpiresAt) {
		return Principal{}, false
	}
	if c.Version == 1 {
		return Principal{SubjectID: "legacy-" + key.TokenSHA256[:12], Kind: "legacy-service", Workspace: key.Workspace, CredentialID: "legacy-" + key.TokenSHA256[:12], Scopes: keyScopes(key)}, true
	}
	for _, subject := range c.Subjects {
		if subject.ID == key.SubjectID && !subject.Disabled && len(membershipScopes(c, subject.ID, key.Workspace)) > 0 {
			return Principal{SubjectID: subject.ID, Kind: subject.Kind, Workspace: key.Workspace, CredentialID: key.ID, Scopes: keyScopes(key)}, true
		}
	}
	return Principal{}, false
}

func principalRole(c Config, p Principal) string {
	if c.Version == 1 {
		if slices.Contains(p.Scopes, "reports:write") {
			return "editor"
		}
		return "viewer"
	}
	for _, member := range c.Memberships {
		if member.SubjectID == p.SubjectID && member.Workspace == p.Workspace {
			return member.Role
		}
	}
	return ""
}

func cloneConfig(c Config) Config {
	c.Subjects = append([]Subject{}, c.Subjects...)
	c.Memberships = append([]Membership{}, c.Memberships...)
	c.CIPublishers = append([]CIPublisher{}, c.CIPublishers...)
	c.Keys = append([]Key{}, c.Keys...)
	for i := range c.Keys {
		c.Keys[i].Scopes = append([]string{}, c.Keys[i].Scopes...)
		if c.Keys[i].ExpiresAt != nil {
			expiry := *c.Keys[i].ExpiresAt
			c.Keys[i].ExpiresAt = &expiry
		}
	}
	if c.CIQuarantineUntil != nil {
		expiry := *c.CIQuarantineUntil
		c.CIQuarantineUntil = &expiry
	}
	return c
}
