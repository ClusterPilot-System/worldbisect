package model

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
	"time"
)

func EnvironmentFromList(values []string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		key, content, found := strings.Cut(value, "=")
		if found {
			result[key] = content
		}
	}
	return result
}

func EnvironmentToList(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func DigestStrings(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func FactorID(kind, key string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + key))
	return kind + ":" + hex.EncodeToString(sum[:8])
}

func IsRedactedValue(value string) bool {
	return strings.HasPrefix(value, "redacted:hmac:")
}

func (captureValue *Capture) Normalize() {
	sort.Slice(captureValue.Workspace.Entries, func(i, j int) bool {
		return captureValue.Workspace.Entries[i].Path < captureValue.Workspace.Entries[j].Path
	})
	sort.Slice(captureValue.Before.Entries, func(i, j int) bool { return captureValue.Before.Entries[i].Path < captureValue.Before.Entries[j].Path })
	sort.Slice(captureValue.ConsultedPaths, func(i, j int) bool { return captureValue.ConsultedPaths[i].Path < captureValue.ConsultedPaths[j].Path })
	sort.Strings(captureValue.EvidenceBoundaries)
	sort.Strings(captureValue.Warnings)
}

func (analysis *Analysis) Normalize() {
	sort.Slice(analysis.Factors, func(i, j int) bool { return analysis.Factors[i].ID < analysis.Factors[j].ID })
	sort.Strings(analysis.CausalFactors)
	sort.Strings(analysis.EvidenceBoundaries)
	sort.Strings(analysis.Limitations)
}

func (manifest WorkspaceManifest) EntryMap() map[string]WorkspaceEntry {
	result := make(map[string]WorkspaceEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		result[entry.Path] = entry
	}
	return result
}

func (entry WorkspaceEntry) Supported() bool {
	return entry.Type == "file" || entry.Type == "dir" || entry.Type == "symlink"
}

func (entry WorkspaceEntry) Equal(other WorkspaceEntry) bool {
	return entry.Path == other.Path && entry.Type == other.Type && entry.Mode == other.Mode && entry.Digest == other.Digest && entry.BlobDigest == other.BlobDigest && entry.LinkTarget == other.LinkTarget
}

func (host HostEvidence) Digest() string {
	return DigestStrings([]string{host.OS, host.Arch, host.Kernel, host.Hostname, host.MountDigest, host.ResourceDigest, host.SecurityDigest})
}

func (limits CaptureLimits) WithDefaults() CaptureLimits {
	if limits.MaxWorkspaceFiles <= 0 {
		limits.MaxWorkspaceFiles = 10000
	}
	if limits.MaxWorkspaceBytes <= 0 {
		limits.MaxWorkspaceBytes = 1 << 30
	}
	if limits.MaxOutputBytes <= 0 {
		limits.MaxOutputBytes = 8 << 20
	}
	if limits.Timeout <= 0 {
		limits.Timeout = 2 * time.Minute
	}
	return limits
}

func CurrentEnvironment() map[string]string {
	return EnvironmentFromList(os.Environ())
}
