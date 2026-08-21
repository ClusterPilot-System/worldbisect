package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func provenAnalysis() *model.Analysis {
	return &model.Analysis{
		ID: "ana_test", GoodCaptureID: "cap_good", BadCaptureID: "cap_bad",
		Status: model.StatusProven, ForwardVerified: true, ReverseVerified: true, MinimalInModel: true,
		CausalFactors: []string{"workspace:config.txt"}, Experiments: make([]model.Experiment, 9),
		Factors:            []model.Factor{{ID: "workspace:config.txt", Type: model.FactorWorkspace, Key: "config.txt", GoodEntry: model.WorkspaceEntry{Type: "file"}}},
		EvidenceBoundaries: []string{"host scheduling was not controlled"},
		Limitations:        []string{"proof applies only to the supported intervention model"},
	}
}

func TestAnalysisReportJSONHasStableContract(t *testing.T) {
	encoded, err := JSON(provenAnalysis())
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"schema_version", "format", "analysis_id", "status", "explanation", "proof", "cause", "boundaries", "limitations", "next_steps", "evidence"} {
		if _, ok := value[field]; !ok {
			t.Fatalf("stable report field %q missing from %s", field, encoded)
		}
	}
	if value["schema_version"] != float64(1) || value["format"] != "worldbisect.analysis-report.v1" || value["status"] != "PROVEN" {
		t.Fatalf("unexpected report identity: %s", encoded)
	}
	if strings.Contains(string(encoded), "good_value") || strings.Contains(string(encoded), "bad_value") {
		t.Fatalf("report exposed internal factor values: %s", encoded)
	}
}

func TestAnalysisReportMarkdownIsGitHubReady(t *testing.T) {
	output := Markdown(provenAnalysis())
	for _, expected := range []string{
		"# WorldBisect diagnosis", "**Status:** `PROVEN`", "## Proof",
		"## Confirmed or suspected cause", "workspace file \"config.txt\"",
		"## Next steps", "## Evidence boundaries", "Analysis `ana_test`",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("markdown does not contain %q:\n%s", expected, output)
		}
	}
}

func TestMarkdownStatusSnapshots(t *testing.T) {
	for _, status := range []model.ProofStatus{model.StatusProven, model.StatusSupported, model.StatusCorrelated, model.StatusUnproven} {
		t.Run(string(status), func(t *testing.T) {
			value := &model.Analysis{ID: "ana_" + strings.ToLower(string(status)), Status: status}
			path := filepath.Join("testdata", "status_"+strings.ToLower(string(status))+".md")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := Markdown(value); string(want) != got {
				t.Fatalf("markdown snapshot changed for %s:\n%s", status, got)
			}
		})
	}
}

func TestEveryStatusHasUnambiguousContractAndMarkdown(t *testing.T) {
	statuses := []model.ProofStatus{model.StatusProven, model.StatusSupported, model.StatusCorrelated, model.StatusUnproven}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			value := &model.Analysis{ID: "ana_" + strings.ToLower(string(status)), Status: status}
			report := Build(value)
			if report.Status != string(status) || report.Explanation == "" {
				t.Fatalf("status is not explicit: %#v", report)
			}
			if !strings.Contains(Markdown(value), "**Status:** `"+string(status)+"`") {
				t.Fatalf("markdown status missing for %s", status)
			}
		})
	}
}

func TestAnalysisReportIsDeterministic(t *testing.T) {
	firstOutput, err := JSON(provenAnalysis())
	if err != nil {
		t.Fatal(err)
	}
	second := provenAnalysis()
	second.CausalFactors = []string{"workspace:config.txt"}
	second.EvidenceBoundaries = []string{"host scheduling was not controlled"}
	second.Limitations = []string{"proof applies only to the supported intervention model"}
	secondOutput, err := JSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstOutput) != string(secondOutput) {
		t.Fatalf("report changed for equivalent inputs:\n%s\n%s", firstOutput, secondOutput)
	}
}
