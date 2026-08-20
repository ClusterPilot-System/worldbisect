package report

import (
	"fmt"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

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
	var builder strings.Builder
	fmt.Fprintln(&builder, "WorldBisect diagnosis")
	fmt.Fprintln(&builder, "=====================")
	fmt.Fprintf(&builder, "result: %s\n\n", humanStatus(value.Status))

	fmt.Fprintln(&builder, "What this means:")
	fmt.Fprintln(&builder, humanExplanation(value.Status))
	fmt.Fprintln(&builder)

	fmt.Fprintln(&builder, "Detected cause:")
	if len(value.CausalFactors) > 0 {
		byID := make(map[string]model.Factor)
		for _, factor := range value.Factors {
			byID[factor.ID] = factor
		}
		for _, identifier := range value.CausalFactors {
			factor := byID[identifier]
			fmt.Fprintf(&builder, "- %s\n", humanFactor(factor))
		}
	} else {
		fmt.Fprintln(&builder, "- No cause was confirmed within the supported test model.")
	}
	fmt.Fprintln(&builder)

	fmt.Fprintln(&builder, "Why this conclusion is trusted:")
	for _, reason := range proofReasons(value) {
		fmt.Fprintf(&builder, "- %s\n", reason)
	}

	fmt.Fprintln(&builder)
	fmt.Fprintln(&builder, "Next steps:")
	for index, step := range nextSteps(value) {
		fmt.Fprintf(&builder, "%d. %s\n", index+1, step)
	}

	if value.Summary != "" && value.Status != model.StatusProven {
		fmt.Fprintf(&builder, "\nTechnical conclusion: %s\n", value.Summary)
	}
	if len(value.Limitations) > 0 {
		fmt.Fprintln(&builder, "\nLimitations:")
		for _, item := range value.Limitations {
			fmt.Fprintln(&builder, "-", item)
		}
	}
	if len(value.EvidenceBoundaries) > 0 {
		fmt.Fprintln(&builder, "\nEvidence boundaries:")
		for _, item := range value.EvidenceBoundaries {
			fmt.Fprintln(&builder, "-", item)
		}
	}

	fmt.Fprintln(&builder, "\nTechnical details (for support):")
	fmt.Fprintf(&builder, "analysis id: %s\n", value.ID)
	fmt.Fprintf(&builder, "captures: good=%s bad=%s\n", value.GoodCaptureID, value.BadCaptureID)
	fmt.Fprintf(&builder, "validated factors: %d; experiments: %d\n", len(value.Factors), len(value.Experiments))
	return builder.String()
}

func humanStatus(status model.ProofStatus) string {
	switch status {
	case model.StatusProven:
		return "PROVEN — cause confirmed"
	case model.StatusSupported:
		return "SUPPORTED — evidence supports this cause, but proof is incomplete"
	case model.StatusCorrelated:
		return "CORRELATED — this difference tracks the failure, but could not be controlled"
	case model.StatusUnproven:
		return "UNPROVEN — no reliable cause was confirmed"
	default:
		return string(status)
	}
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

func proofReasons(value *model.Analysis) []string {
	if value.Status != model.StatusProven {
		return []string{
			fmt.Sprintf("WorldBisect ran %d controlled experiment(s).", len(value.Experiments)),
			"The result is limited to the supported factors and evidence boundaries listed below.",
		}
	}
	return []string{
		"The bad run was repaired when the detected factor was changed to the good value.",
		"The failure returned when the same factor was changed back in the opposite direction.",
		"The factor set was minimal within the tested model.",
		fmt.Sprintf("The conclusion was checked with %d controlled experiment(s).", len(value.Experiments)),
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
