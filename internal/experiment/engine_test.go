package experiment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/capture"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestEngineProvesFileCause(t *testing.T) {
	root := t.TempDir()
	goodRoot := filepath.Join(root, "good")
	badRoot := filepath.Join(root, "bad")
	for _, directory := range []string{goodRoot, badRoot} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(directory, "check.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\ngrep -qx 'mode=good' config.txt\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(goodRoot, "config.txt"), []byte("mode=good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badRoot, "config.txt"), []byte("mode=bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	capturer := capture.New(dataStore, runner.New())
	oracleValue := model.Oracle{Kind: "exit", ExpectedExitCode: intPointer(0)}
	good, err := capturer.Capture(context.Background(), capture.Request{Workspace: goodRoot, Command: model.CommandSpec{Arguments: []string{"./check.sh"}, Directory: goodRoot, TimeoutMS: time.Second.Milliseconds(), Environment: map[string]string{}}, Oracle: oracleValue})
	if err != nil {
		t.Fatal(err)
	}
	bad, _ := capturer.Capture(context.Background(), capture.Request{Workspace: badRoot, Command: model.CommandSpec{Arguments: []string{"./check.sh"}, Directory: badRoot, TimeoutMS: time.Second.Milliseconds(), Environment: map[string]string{}}, Oracle: oracleValue})
	analysis, err := New(dataStore, runner.New()).Analyze(context.Background(), Request{Good: good, Bad: bad, Command: []string{"./check.sh"}, Repetitions: 2, MaxExperiments: 64})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != model.StatusProven {
		t.Fatalf("status = %s limitations=%v", analysis.Status, analysis.Limitations)
	}
	if len(analysis.CausalFactors) != 1 {
		t.Fatalf("causal factors = %v", analysis.CausalFactors)
	}
}

func intPointer(value int) *int { return &value }
