package capture

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestCaptureRedactsSecretEnvironment(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	capturer := New(dataStore, runner.New())
	record, err := capturer.Capture(context.Background(), Request{
		Workspace: root,
		Command: model.CommandSpec{
			Arguments:   []string{script},
			Directory:   root,
			TimeoutMS:   time.Second.Milliseconds(),
			Environment: map[string]string{"API_TOKEN": "super-secret", "MODE": "good"},
		},
		Oracle: model.Oracle{Kind: "exit", ExpectedExitCode: intPointer(0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Command.Environment["API_TOKEN"] == "super-secret" {
		t.Fatal("secret was not redacted")
	}
	if record.Command.Environment["MODE"] != "good" {
		t.Fatal("non-secret environment changed")
	}
	if len(record.SecretEvidence) != 1 || record.SecretEvidence[0].Fingerprint == "" {
		t.Fatal("secret evidence missing")
	}
}

func intPointer(value int) *int { return &value }
