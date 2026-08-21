package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

const AnalysisReportSchemaVersion = 1

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

	steps := make([]string, 0, len(value.CausalFactors)+3)
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
