package report

import (
	"strings"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func TestAnalysisIsReadableAndActionableForProvenWorkspaceCause(t *testing.T) {
	value := &model.Analysis{
		ID:              "ana_test",
		GoodCaptureID:   "cap_good",
		BadCaptureID:    "cap_bad",
		Status:          model.StatusProven,
		CausalFactors:   []string{"workspace:config.txt"},
		Experiments:     make([]model.Experiment, 9),
		ForwardVerified: true,
		ReverseVerified: true,
		MinimalInModel:  true,
		Factors: []model.Factor{{
			ID:        "workspace:config.txt",
			Type:      model.FactorWorkspace,
			Key:       "config.txt",
			GoodEntry: model.WorkspaceEntry{Type: "file"},
		}},
	}

	output := Analysis(value)
	for _, expected := range []string{
		"result: PROVEN — cause confirmed",
		"workspace file \"config.txt\" differs between the successful and failing run",
		"The bad run was repaired when the detected factor was changed to the good value.",
		"1. Make a backup of \"config.txt\" before editing or replacing it.",
		"2. Open \"config.txt\" in the failing workspace",
		"Run the original command again and confirm that the oracle passes.",
		"analysis id: ana_test",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "causal factors:") || strings.Contains(output, "factors: 1 experiments: 9") {
		t.Fatalf("output still uses the old raw-only presentation:\n%s", output)
	}
}

func TestAnalysisExplainsUnprovenWithoutInventingCause(t *testing.T) {
	output := Analysis(&model.Analysis{ID: "ana_unproven", Status: model.StatusUnproven})
	for _, expected := range []string{
		"result: UNPROVEN — no reliable cause was confirmed",
		"No cause was confirmed within the supported test model.",
		"Capture a new good run and bad run",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output)
		}
	}
}
