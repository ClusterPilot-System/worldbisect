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
	fmt.Fprintf(&builder, "analysis: %s\n", value.ID)
	fmt.Fprintf(&builder, "status: %s\n", value.Status)
	fmt.Fprintf(&builder, "good: %s\nbad: %s\n", value.GoodCaptureID, value.BadCaptureID)
	fmt.Fprintf(&builder, "factors: %d experiments: %d\n", len(value.Factors), len(value.Experiments))
	if len(value.CausalFactors) > 0 {
		fmt.Fprintln(&builder, "causal factors:")
		byID := make(map[string]model.Factor)
		for _, factor := range value.Factors {
			byID[factor.ID] = factor
		}
		for _, identifier := range value.CausalFactors {
			factor := byID[identifier]
			fmt.Fprintf(&builder, "  - %s %s (%s)\n", factor.Type, factor.Key, identifier)
		}
	}
	if value.Summary != "" {
		fmt.Fprintln(&builder, "summary:", value.Summary)
	}
	if len(value.Limitations) > 0 {
		fmt.Fprintln(&builder, "limitations:")
		for _, item := range value.Limitations {
			fmt.Fprintln(&builder, "  -", item)
		}
	}
	if len(value.EvidenceBoundaries) > 0 {
		fmt.Fprintln(&builder, "evidence boundaries:")
		for _, item := range value.EvidenceBoundaries {
			fmt.Fprintln(&builder, "  -", item)
		}
	}
	return builder.String()
}
