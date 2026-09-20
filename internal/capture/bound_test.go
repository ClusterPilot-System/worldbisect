package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestCaptureWithBindingUsesSuppliedDirectoryAndCapturedOracle(t *testing.T) {
	directory := t.TempDir()
	os.WriteFile(filepath.Join(directory, "input.txt"), []byte("input"), 0600)
	directoryFile, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer directoryFile.Close()
	executable, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	executableFile, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer executableFile.Close()
	identity, err := runner.Identity(executableFile)
	if err != nil {
		t.Fatal(err)
	}
	binding := &runner.ExecutionBinding{ExecutableFile: executableFile, DirectoryFile: directoryFile, ExecutablePath: executable, DirectoryPath: directory, Identity: identity}
	data, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("complete"))
	// The explicit binding determines the workspace. Request labels need not name
	// an existing directory and must not be opened as an alternate source.
	record, err := New(data, runner.New()).CaptureWithBinding(context.Background(), Request{
		Workspace: filepath.Join(t.TempDir(), "descriptive-label-only"),
		Command:   model.CommandSpec{Arguments: []string{executable, "-c", "printf complete > result.txt"}, Directory: "unused-description", TimeoutMS: 1000},
		Oracle:    model.Oracle{Kind: "file_digest", File: "result.txt", Digest: hex.EncodeToString(sum[:])}, TraceMode: "off",
	}, binding)
	if err != nil {
		t.Fatal(err)
	}
	if record.WorkspaceRoot != directory || record.Command.Directory != directory {
		t.Fatal("capture metadata did not use binding")
	}
	if !record.OracleResult.Passed {
		t.Fatalf("bound file oracle failed: %#v", record.OracleResult)
	}
	if len(record.Before.Entries) != 1 || len(record.Workspace.Entries) != 2 {
		t.Fatalf("unexpected workspace capture: %v -> %v", record.Before.Entries, record.Workspace.Entries)
	}
	for _, entry := range record.Workspace.Entries {
		if entry.Path == "result.txt" {
			b, err := data.GetBlob(entry.BlobDigest)
			if err != nil || string(b) != "complete" {
				t.Fatal(fmt.Sprintf("unexpected captured output %q %v", b, err))
			}
		}
	}
}

func TestManifestFileOracleRequiresCapturedRegularFile(t *testing.T) {
	manifest := model.WorkspaceManifest{Entries: []model.WorkspaceEntry{{Path: "result", Type: "file", Digest: "expected"}, {Path: "link", Type: "symlink", LinkTarget: "result"}}}
	if !evaluateManifestFileOracle(model.Oracle{File: "result", Digest: "expected"}, manifest).Passed {
		t.Fatal("regular captured file not evaluated")
	}
	for _, path := range []string{"", ".", "../result", "/result", "link", "missing"} {
		if evaluateManifestFileOracle(model.Oracle{File: path, Digest: "expected"}, manifest).Passed {
			t.Fatalf("uncaptured regular file accepted: %q", path)
		}
	}
}
