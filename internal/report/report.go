package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

const AnalysisReportSchemaVersion = 1

type OutputLinks struct {
	ReportURL string
	BundleURL string
}

type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Properties []junitProperty `xml:"properties>property"`
	Cases      []junitTestCase `xml:"testcase"`
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitTestCase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

type sarifDocument struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	ShortDescription sarifText `json:"shortDescription"`
	HelpURI          string    `json:"helpUri,omitempty"`
}

type sarifResult struct {
	RuleID     string         `json:"ruleId"`
	Level      string         `json:"level"`
	Message    sarifText      `json:"message"`
	Properties map[string]any `json:"properties"`
}

type sarifText struct {
	Text string `json:"text"`
}

// AnalysisReport is the stable, secret-safe public representation of an analysis.
// Keep this separate from model.Analysis so persisted internals can evolve without
// changing the automation and support contract emitted by the CLI.
type AnalysisReport struct {
	SchemaVersion int            `json:"schema_version"`
	Format        string         `json:"format"`
	AnalysisID    string         `json:"analysis_id"`
	Status        string         `json:"status"`
	Explanation   string         `json:"explanation"`
	Proof         ReportProof    `json:"proof"`
	Cause         []ReportFactor `json:"cause"`
	Boundaries    []string       `json:"boundaries"`
	Limitations   []string       `json:"limitations"`
	NextSteps     []string       `json:"next_steps"`
	Evidence      ReportEvidence `json:"evidence"`
	Summary       string         `json:"summary,omitempty"`
}

type ReportProof struct {
	ForwardVerified bool `json:"forward_verified"`
	ReverseVerified bool `json:"reverse_verified"`
	MinimalInModel  bool `json:"minimal_in_model"`
}

type ReportFactor struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Key         string `json:"key"`
	Description string `json:"description"`
}

type ReportEvidence struct {
	GoodCaptureID   string `json:"good_capture_id"`
	BadCaptureID    string `json:"bad_capture_id"`
	FactorCount     int    `json:"factor_count"`
	ExperimentCount int    `json:"experiment_count"`
}

// Build derives every supported output format from one deterministic report value.
func Build(value *model.Analysis) AnalysisReport {
	byID := make(map[string]model.Factor, len(value.Factors))
	for _, factor := range value.Factors {
		byID[factor.ID] = factor
	}

	identifiers := append([]string(nil), value.CausalFactors...)
	sort.Strings(identifiers)
	cause := make([]ReportFactor, 0, len(identifiers))
	for _, identifier := range identifiers {
		factor, found := byID[identifier]
		if !found {
			cause = append(cause, ReportFactor{ID: identifier, Description: "factor metadata was not available in this analysis"})
			continue
		}
		cause = append(cause, ReportFactor{
			ID: identifier, Type: string(factor.Type), Key: factor.Key,
			Description: humanFactor(factor),
		})
	}

	boundaries := append(make([]string, 0, len(value.EvidenceBoundaries)), value.EvidenceBoundaries...)
	limitations := append(make([]string, 0, len(value.Limitations)), value.Limitations...)
	sort.Strings(boundaries)
	sort.Strings(limitations)
	return AnalysisReport{
		SchemaVersion: AnalysisReportSchemaVersion,
		Format:        "worldbisect.analysis-report.v1",
		AnalysisID:    value.ID,
		Status:        string(value.Status),
		Explanation:   humanExplanation(value.Status),
		Proof: ReportProof{
			ForwardVerified: value.ForwardVerified,
			ReverseVerified: value.ReverseVerified,
			MinimalInModel:  value.MinimalInModel,
		},
		Cause:       cause,
		Boundaries:  boundaries,
		Limitations: limitations,
		NextSteps:   nextSteps(value),
		Evidence: ReportEvidence{
			GoodCaptureID:   value.GoodCaptureID,
			BadCaptureID:    value.BadCaptureID,
			FactorCount:     len(value.Factors),
			ExperimentCount: len(value.Experiments),
		},
		Summary: value.Summary,
	}
}

// JSON returns the versioned machine-readable report contract.
func JSON(value *model.Analysis) ([]byte, error) {
	encoded, err := json.MarshalIndent(Build(value), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// JUnit returns a deterministic test result for CI systems that consume XML.
// PROVEN and SUPPORTED are findings; CORRELATED and UNPROVEN are explicit skips.
func JUnit(value *model.Analysis, links OutputLinks) ([]byte, error) {
	report := Build(value)
	caseValue := junitTestCase{Classname: "worldbisect.analysis", Name: report.AnalysisID}
	properties := []junitProperty{
		{Name: "analysis_id", Value: report.AnalysisID},
		{Name: "status", Value: report.Status},
		{Name: "schema_version", Value: fmt.Sprint(report.SchemaVersion)},
	}
	if links.ReportURL != "" {
		properties = append(properties, junitProperty{Name: "report_url", Value: links.ReportURL})
	}
	if links.BundleURL != "" {
		properties = append(properties, junitProperty{Name: "bundle_url", Value: links.BundleURL})
	}
	failures := 0
	skipped := 0
	if findingStatus(value.Status) {
		failures = 1
		caseValue.Failure = &junitFailure{Message: report.Status, Text: report.Explanation}
	} else {
		skipped = 1
		caseValue.Skipped = &junitSkipped{Message: report.Status}
	}
	document := junitTestSuites{
		Name: "WorldBisect", Tests: 1, Failures: failures, Skipped: skipped,
		Suites: []junitTestSuite{{
			Name: "worldbisect.analysis", Tests: 1, Failures: failures, Skipped: skipped,
			Properties: properties, Cases: []junitTestCase{caseValue},
		}},
	}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"), append(encoded, '\n')...), nil
}

// SARIF returns a SARIF 2.1.0 finding suitable for GitHub code scanning upload.
func SARIF(value *model.Analysis, links OutputLinks) ([]byte, error) {
	report := Build(value)
	ruleID := "worldbisect/" + report.Status
	rule := sarifRule{
		ID: ruleID, Name: "WorldBisect " + report.Status,
		ShortDescription: sarifText{Text: report.Explanation},
		HelpURI:          links.ReportURL,
	}
	properties := map[string]any{
		"analysis_id":      report.AnalysisID,
		"status":           report.Status,
		"schema_version":   report.SchemaVersion,
		"forward_verified": report.Proof.ForwardVerified,
		"reverse_verified": report.Proof.ReverseVerified,
		"minimal_in_model": report.Proof.MinimalInModel,
	}
	if links.BundleURL != "" {
		properties["bundle_url"] = links.BundleURL
	}
	if links.ReportURL != "" {
		properties["report_url"] = links.ReportURL
	}
	result := sarifResult{RuleID: ruleID, Level: sarifLevel(value.Status), Message: sarifText{Text: report.Explanation}, Properties: properties}
	document := sarifDocument{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs:    []sarifRun{{Tool: sarifTool{Driver: sarifDriver{Name: "WorldBisect", Rules: []sarifRule{rule}}}, Results: []sarifResult{result}}},
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// ShouldFail applies the documented CI policy without changing the report payload.
func ShouldFail(status model.ProofStatus, policy string) (bool, error) {
	switch policy {
	case "never", "":
		return false, nil
	case "proven":
		return status == model.StatusProven, nil
	case "supported":
		return status == model.StatusProven || status == model.StatusSupported, nil
	case "correlated":
		return status != model.StatusUnproven, nil
	case "any":
		return true, nil
	default:
		return false, fmt.Errorf("unsupported --fail-on value %q (use never, proven, supported, correlated, or any)", policy)
	}
}

func findingStatus(status model.ProofStatus) bool {
	return status == model.StatusProven || status == model.StatusSupported
}

func sarifLevel(status model.ProofStatus) string {
	switch status {
	case model.StatusProven:
		return "error"
	case model.StatusSupported, model.StatusCorrelated:
		return "warning"
	default:
		return "note"
	}
}

// Markdown returns a stable report suitable for GitHub comments and support tickets.
func Markdown(value *model.Analysis) string {
	report := Build(value)
	var builder strings.Builder
	fmt.Fprintln(&builder, "# WorldBisect diagnosis")
	fmt.Fprintf(&builder, "\n**Status:** `%s`\n\n%s\n", report.Status, report.Explanation)
	fmt.Fprintln(&builder, "## Proof")
	fmt.Fprintf(&builder, "\n- Forward intervention verified: `%t`\n- Reverse intervention verified: `%t`\n- Minimal within tested model: `%t`\n", report.Proof.ForwardVerified, report.Proof.ReverseVerified, report.Proof.MinimalInModel)
	fmt.Fprintln(&builder, "\n## Confirmed or suspected cause")
	if len(report.Cause) == 0 {
		fmt.Fprintln(&builder, "\nNo cause was confirmed within the supported test model.")
	} else {
		for _, factor := range report.Cause {
			fmt.Fprintf(&builder, "\n- `%s` — %s", factor.ID, factor.Description)
		}
		fmt.Fprintln(&builder)
	}
	fmt.Fprintln(&builder, "\n## Next steps")
	for index, step := range report.NextSteps {
		fmt.Fprintf(&builder, "%d. %s\n", index+1, step)
	}
	if len(report.Boundaries) > 0 {
		fmt.Fprintln(&builder, "\n## Evidence boundaries")
		for _, boundary := range report.Boundaries {
			fmt.Fprintf(&builder, "\n- %s", boundary)
		}
		fmt.Fprintln(&builder)
	}
	if len(report.Limitations) > 0 {
		fmt.Fprintln(&builder, "\n## Limitations")
		for _, limitation := range report.Limitations {
			fmt.Fprintf(&builder, "\n- %s", limitation)
		}
		fmt.Fprintln(&builder)
	}
	fmt.Fprintf(&builder, "\n<sub>Analysis `%s`; experiments: %d; factors: %d.</sub>\n", report.AnalysisID, report.Evidence.ExperimentCount, report.Evidence.FactorCount)
	return builder.String()
}

func Capture(value *model.Capture) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "capture: %s\n", value.ID)
	fmt.Fprintf(&builder, "oracle: %t (%s)\n", value.OracleResult.Passed, value.OracleResult.Detail)
	fmt.Fprintf(&builder, "exit: %d timeout: %t duration_ms: %d\n", value.Result.ExitCode, value.Result.TimedOut, value.Result.DurationMS)
	fmt.Fprintf(&builder, "workspace: %d objects, %d bytes\n", value.Workspace.TotalFiles, value.Workspace.TotalBytes)
	if len(value.EvidenceBoundaries) > 0 {
		fmt.Fprintln(&builder, "evidence boundaries:")
		for _, item := range value.EvidenceBoundaries {
			fmt.Fprintln(&builder, "  -", item)
		}
	}
	return builder.String()
}

func Analysis(value *model.Analysis) string {
	return Markdown(value)
}

func humanExplanation(status model.ProofStatus) string {
	switch status {
	case model.StatusProven:
		return "The failing run was repaired by changing the detected factor, and the failure returned when that change was reversed. The factor was also minimal within the tested model."
	case model.StatusSupported:
		return "The detected factor repaired the failure in the forward test, but the reverse or minimality proof was incomplete. Treat it as the best current lead, not as final proof."
	case model.StatusCorrelated:
		return "The detected difference appeared together with the failure, but WorldBisect could not safely change it independently. It is a lead for investigation, not a confirmed cause."
	default:
		return "The available evidence was not sufficient to make a sound causal claim. Review the limitations and collect more controlled captures."
	}
}

func humanFactor(factor model.Factor) string {
	switch factor.Type {
	case model.FactorWorkspace:
		return fmt.Sprintf("workspace %s %q differs between the successful and failing run", workspaceKind(factor), factor.Key)
	case model.FactorEnvironment:
		return fmt.Sprintf("environment variable %q differs between the successful and failing run", factor.Key)
	default:
		return fmt.Sprintf("%s %q differs between the successful and failing run", factor.Type, factor.Key)
	}
}

func workspaceKind(factor model.Factor) string {
	entry := factor.GoodEntry
	if entry.Type == "" {
		entry = factor.BadEntry
	}
	switch entry.Type {
	case "directory":
		return "directory"
	case "symlink":
		return "symbolic link"
	default:
		return "file"
	}
}

func nextSteps(value *model.Analysis) []string {
	if len(value.CausalFactors) == 0 {
		return []string{
			"Read the Limitations and Evidence boundaries sections below.",
			"Capture a new good run and bad run with the same command, oracle, workspace scope, and stable inputs.",
			"Run the comparison again after removing uncontrolled differences where possible.",
		}
	}

	// Avoid deriving an allocation capacity from untrusted collection lengths.
	steps := make([]string, 0)
	byID := make(map[string]model.Factor, len(value.Factors))
	for _, factor := range value.Factors {
		byID[factor.ID] = factor
	}
	for _, identifier := range value.CausalFactors {
		factor := byID[identifier]
		switch factor.Type {
		case model.FactorWorkspace:
			steps = append(steps,
				fmt.Sprintf("Make a backup of %q before editing or replacing it.", factor.Key),
				fmt.Sprintf("Open %q in the failing workspace and compare it with the known-good copy; check the content, file presence, permissions, and link target if applicable.", factor.Key),
				fmt.Sprintf("Restore or correct %q so it matches the known-good configuration. Do not change unrelated files.", factor.Key),
			)
		case model.FactorEnvironment:
			steps = append(steps,
				fmt.Sprintf("Record the current value of environment variable %q without sharing secrets.", factor.Key),
				fmt.Sprintf("Set %q to the known-good value for the next test run.", factor.Key),
				"Repeat the command with the same workspace and oracle.",
			)
		default:
			steps = append(steps, fmt.Sprintf("Compare %q with the known-good run and restore the known-good value.", factor.Key))
		}
	}
	steps = append(steps,
		"Run the original command again and confirm that the oracle passes.",
		"If the command still fails, create fresh good and bad captures and attach this analysis ID when contacting support.",
	)
	return steps
}
