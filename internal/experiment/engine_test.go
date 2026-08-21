package experiment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	progressEvents := 0
	analysis, err := New(dataStore, runner.New()).Analyze(context.Background(), Request{Good: good, Bad: bad, Command: []string{"./check.sh"}, Repetitions: 2, MaxExperiments: 64, Progress: func(ProgressEvent) { progressEvents++ }})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != model.StatusProven {
		t.Fatalf("status = %s limitations=%v", analysis.Status, analysis.Limitations)
	}
	if len(analysis.CausalFactors) != 1 {
		t.Fatalf("causal factors = %v", analysis.CausalFactors)
	}
	if progressEvents == 0 {
		t.Fatal("analysis did not emit progress events")
	}
	cachedAnalysis, err := New(dataStore, runner.New()).Analyze(context.Background(), Request{Good: good, Bad: bad, Command: []string{"./check.sh"}, Repetitions: 2, MaxExperiments: 64})
	if err != nil {
		t.Fatal(err)
	}
	cacheHits := 0
	for _, experiment := range cachedAnalysis.Experiments {
		if experiment.CacheHit {
			cacheHits++
		}
	}
	if cacheHits == 0 {
		t.Fatalf("second equivalent analysis did not reuse experiments: %+v", cachedAnalysis.Experiments)
	}
	if cachedAnalysis.Status != model.StatusProven {
		t.Fatalf("cached status = %s limitations=%v", cachedAnalysis.Status, cachedAnalysis.Limitations)
	}
}

func TestCancelledAnalysisPersistsCompletedState(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Unix(1, 0).UTC()
	good := &model.Capture{SchemaVersion: 3, ID: "good-cancel", CreatedAt: createdAt, Command: model.CommandSpec{Arguments: []string{"check"}}}
	bad := &model.Capture{SchemaVersion: 3, ID: "bad-cancel", CreatedAt: createdAt, Command: model.CommandSpec{Arguments: []string{"check"}}}
	if err := dataStore.SaveCapture(good); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveCapture(bad); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	analysis, err := New(dataStore, runner.New()).Analyze(ctx, Request{Good: good, Bad: bad, Command: []string{"check"}})
	if !errors.Is(err, context.Canceled) || analysis == nil {
		t.Fatalf("cancelled analysis = %+v, %v", analysis, err)
	}
	if len(analysis.Limitations) == 0 || !strings.Contains(analysis.Limitations[len(analysis.Limitations)-1], "completed experiments were retained") {
		t.Fatalf("cancellation guidance missing: %+v", analysis.Limitations)
	}
	if _, err := dataStore.LoadAnalysis(analysis.ID); err != nil {
		t.Fatalf("cancelled analysis was not persisted: %v", err)
	}
}

func intPointer(value int) *int { return &value }
