package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/artifact"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func TestHandoffRequiresPreviewAndConfirmation(t *testing.T) {
	dataStore, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	good := &model.Capture{ID: "good", CreatedAt: createdAt}
	bad := &model.Capture{ID: "bad", CreatedAt: createdAt}
	if err := dataStore.SaveCapture(good); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.SaveCapture(bad); err != nil {
		t.Fatal(err)
	}
	analysis := &model.Analysis{ID: "analysis", CreatedAt: createdAt, GoodCaptureID: good.ID, BadCaptureID: bad.ID, Status: model.StatusProven}
	if err := dataStore.SaveAnalysis(analysis); err != nil {
		t.Fatal(err)
	}

	var previewOutput bytes.Buffer
	if err := run([]string{"handoff", "--store", dataStore.Root(), "--analysis", analysis.ID, "--preview"}, &previewOutput, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var preview artifact.DiagnosticPreview
	if err := json.Unmarshal(previewOutput.Bytes(), &preview); err != nil || preview.IncidentID == "" || !preview.ConfirmationRequired {
		t.Fatalf("invalid preview: %s %v", previewOutput.String(), err)
	}

	output := filepath.Join(t.TempDir(), "handoff.wdiag")
	if err := run([]string{"handoff", "--store", dataStore.Root(), "--analysis", analysis.ID, "--output", output}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("missing confirmation was accepted: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("handoff was written without confirmation: %v", err)
	}

	var result bytes.Buffer
	if err := run([]string{"handoff", "--store", dataStore.Root(), "--analysis", analysis.ID, "--output", output, "--confirm"}, &result, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.String(), preview.IncidentID) {
		t.Fatalf("handoff result omitted incident ID: %s", result.String())
	}
}
