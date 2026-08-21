package report

import (
	"encoding/json"
	"encoding/xml"
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
		CausalFactors: []string{"workspace:config.txt"}, Experiments: []model.Experiment{{CacheHit: true}, {}, {}},
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
	evidence, ok := value["evidence"].(map[string]any)
	if !ok || evidence["cache_hits"] != float64(1) || evidence["cache_misses"] != float64(2) {
		t.Fatalf("cache evidence = %+v", value["evidence"])
	}
}

func TestAnalysisReportMarkdownIsGitHubReady(t *testing.T) {
	output := Markdown(provenAnalysis())
	for _, expected := range []string{
		"# WorldBisect diagnosis", "**Status:** `PROVEN`", "## Proof",
		"## Confirmed or suspected cause", "workspace file \"config.txt\"",
		"## Next steps", "## Evidence boundaries", "cache hits: 1; cache misses: 2", "Analysis `ana_test`",
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

func TestDirectoryFactorUsesPlainLanguage(t *testing.T) {
	value := &model.Analysis{ID: "ana_directory", Status: model.StatusSupported, CausalFactors: []string{"workspace:config"}, Factors: []model.Factor{{ID: "workspace:config", Type: model.FactorWorkspace, Key: "config", GoodEntry: model.WorkspaceEntry{Type: "dir"}}}}
	if !strings.Contains(Markdown(value), `workspace directory "config"`) {
		t.Fatalf("directory factor was not described plainly: %s", Markdown(value))
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

func TestJUnitAndSARIFContractsCoverStatuses(t *testing.T) {
	for _, status := range []model.ProofStatus{model.StatusProven, model.StatusSupported, model.StatusCorrelated, model.StatusUnproven} {
		t.Run(string(status), func(t *testing.T) {
			value := &model.Analysis{ID: "ana_" + strings.ToLower(string(status)), Status: status, Summary: "do not expose supersecret"}
			junit, err := JUnit(value, OutputLinks{ReportURL: "https://ci.example/report", BundleURL: "https://ci.example/bundle"})
			if err != nil {
				t.Fatal(err)
			}
			var suites struct {
				Failures int `xml:"failures,attr"`
				Skipped  int `xml:"skipped,attr"`
			}
			if err := xml.Unmarshal(junit, &suites); err != nil {
				t.Fatalf("invalid JUnit: %v\n%s", err, junit)
			}
			if findingStatus(status) && suites.Failures != 1 {
				t.Fatalf("JUnit failures = %d for %s", suites.Failures, status)
			}
			if !findingStatus(status) && suites.Skipped != 1 {
				t.Fatalf("JUnit skipped = %d for %s", suites.Skipped, status)
			}
			sarif, err := SARIF(value, OutputLinks{ReportURL: "https://ci.example/report", BundleURL: "https://ci.example/bundle"})
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Version string `json:"version"`
				Runs    []struct {
					Results []struct {
						RuleID     string         `json:"ruleId"`
						Level      string         `json:"level"`
						Properties map[string]any `json:"properties"`
					} `json:"results"`
				} `json:"runs"`
			}
			if err := json.Unmarshal(sarif, &document); err != nil {
				t.Fatalf("invalid SARIF: %v\n%s", err, sarif)
			}
			if document.Version != "2.1.0" || len(document.Runs) != 1 || len(document.Runs[0].Results) != 1 {
				t.Fatalf("invalid SARIF shape: %s", sarif)
			}
			result := document.Runs[0].Results[0]
			if result.RuleID != "worldbisect/"+string(status) || result.Properties["status"] != string(status) {
				t.Fatalf("invalid SARIF result: %s", sarif)
			}
			if strings.Contains(string(junit), "supersecret") || strings.Contains(string(sarif), "supersecret") {
				t.Fatal("diagnostic formats exposed secret text")
			}
		})
	}
}

func TestShouldFailIsDeterministic(t *testing.T) {
	cases := []struct {
		status model.ProofStatus
		policy string
		want   bool
	}{
		{model.StatusProven, "never", false},
		{model.StatusProven, "proven", true},
		{model.StatusSupported, "proven", false},
		{model.StatusSupported, "supported", true},
		{model.StatusCorrelated, "correlated", true},
		{model.StatusUnproven, "correlated", false},
		{model.StatusUnproven, "any", true},
	}
	for _, test := range cases {
		got, err := ShouldFail(test.status, test.policy)
		if err != nil || got != test.want {
			t.Errorf("ShouldFail(%s, %s) = %t, %v; want %t", test.status, test.policy, got, err, test.want)
		}
	}
	if _, err := ShouldFail(model.StatusProven, "invalid"); err == nil {
		t.Fatal("invalid fail policy was accepted")
	}
}
