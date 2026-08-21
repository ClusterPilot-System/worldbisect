package experiment

import (
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/version"
)

func TestExperimentCacheKeyIsDeterministicAndSemantic(t *testing.T) {
	good := &model.Capture{
		ID: "cap-a", CreatedAt: time.Unix(1, 0), Workspace: model.WorkspaceManifest{
			Root: "/different/root/a", Digest: "world-good", Entries: []model.WorkspaceEntry{{Path: "config.txt", Type: "file", Mode: 0o644, Digest: "digest-good"}},
		}, Command: model.CommandSpec{Arguments: []string{"./check"}, Environment: map[string]string{"B": "2", "A": "1"}, TimeoutMS: 1000}, Oracle: model.Oracle{Kind: "exit", ExpectedExitCode: intPointer(0)},
	}
	bad := &model.Capture{ID: "cap-b", Workspace: model.WorkspaceManifest{Root: "/different/root/b", Digest: "world-bad", Entries: []model.WorkspaceEntry{{Path: "config.txt", Type: "file", Mode: 0o644, Digest: "digest-bad"}}}, Command: good.Command, Oracle: good.Oracle}
	factor := model.Factor{ID: "workspace:config.txt", Type: model.FactorWorkspace, Key: "config.txt", GoodEntry: good.Workspace.Entries[0], BadEntry: bad.Workspace.Entries[0]}
	first := experimentCacheKey(bad, good, good.ID, good.Command.Arguments, []model.Factor{factor}, "intervention")
	second := experimentCacheKey(bad, good, good.ID, good.Command.Arguments, []model.Factor{factor}, "intervention")
	if first != second {
		t.Fatalf("equivalent cache keys differ: %s != %s", first, second)
	}
	changed := factor
	changed.BadEntry.Digest = "different"
	if first == experimentCacheKey(bad, good, good.ID, good.Command.Arguments, []model.Factor{changed}, "intervention") {
		t.Fatal("semantic factor change did not invalidate cache key")
	}
	if version.Version == "" {
		t.Fatal("tool version must participate in cache contract")
	}
}
