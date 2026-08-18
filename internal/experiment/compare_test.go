package experiment

import (
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func TestCompareEnvironmentAndWorkspace(t *testing.T) {
	good := &model.Capture{
		Command: model.CommandSpec{Environment: map[string]string{"MODE": "good", "API_TOKEN": "redacted:hmac:a"}},
		Workspace: model.WorkspaceManifest{Entries: []model.WorkspaceEntry{{Path: "config", Type: "file", Digest: "good", BlobDigest: "good"}}},
		SecretEvidence: []model.SecretEvidence{{Name: "API_TOKEN"}},
	}
	bad := &model.Capture{
		Command: model.CommandSpec{Environment: map[string]string{"MODE": "bad", "API_TOKEN": "redacted:hmac:b"}},
		Workspace: model.WorkspaceManifest{Entries: []model.WorkspaceEntry{{Path: "config", Type: "file", Digest: "bad", BlobDigest: "bad"}}},
	}
	factors, boundaries := Compare(good, bad, 10)
	if len(factors) != 2 {
		t.Fatalf("factors = %d", len(factors))
	}
	if len(boundaries) == 0 {
		t.Fatal("secret boundary missing")
	}
}
