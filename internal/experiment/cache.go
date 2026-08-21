package experiment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/version"
)

const experimentCacheContract = "worldbisect-experiment-cache-v1"

type cacheManifest struct {
	Digest  string                 `json:"digest"`
	Entries []model.WorkspaceEntry `json:"entries"`
}

type cacheInput struct {
	Contract        string            `json:"contract"`
	ToolVersion     string            `json:"tool_version"`
	ToolCommit      string            `json:"tool_commit"`
	BaseWorld       cacheManifest     `json:"base_world"`
	SourceWorld     cacheManifest     `json:"source_world"`
	BaseEnvironment map[string]string `json:"base_environment"`
	Command         []string          `json:"command"`
	TimeoutMS       int64             `json:"timeout_ms"`
	Oracle          model.Oracle      `json:"oracle"`
	Factors         []model.Factor    `json:"factors"`
	BaseIsGood      bool              `json:"base_is_good"`
	Kind            string            `json:"kind"`
}

func experimentCacheKey(base, source *model.Capture, goodCaptureID string, command []string, selected []model.Factor, kind string) string {
	environment := make(map[string]string, len(base.Command.Environment))
	for key, value := range base.Command.Environment {
		environment[key] = value
	}
	input := cacheInput{
		Contract:        experimentCacheContract,
		ToolVersion:     version.Version,
		ToolCommit:      version.Commit,
		BaseWorld:       manifestForCache(base.Workspace),
		SourceWorld:     manifestForCache(source.Workspace),
		BaseEnvironment: environment,
		Command:         append([]string{}, command...),
		TimeoutMS:       base.Command.TimeoutMS,
		Oracle:          base.Oracle,
		Factors:         append([]model.Factor{}, selected...),
		BaseIsGood:      base.ID == goodCaptureID,
		Kind:            kind,
	}
	sort.Slice(input.Factors, func(i, j int) bool { return input.Factors[i].ID < input.Factors[j].ID })
	encoded, _ := json.Marshal(input)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func manifestForCache(manifest model.WorkspaceManifest) cacheManifest {
	entries := append([]model.WorkspaceEntry{}, manifest.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return cacheManifest{Digest: manifest.Digest, Entries: entries}
}
