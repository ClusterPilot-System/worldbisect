package experiment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/id"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/oracle"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
	"github.com/ClusterPilot-System/worldbisect/internal/workspace"
)

type Request struct {
	Good           *model.Capture
	Bad            *model.Capture
	Command        []string
	Repetitions    int
	MaxExperiments int
	MaxFactors     int
	MaxOutputBytes int64
}

type Engine struct {
	store  *store.Store
	runner *runner.Runner
}

func New(dataStore *store.Store, commandRunner *runner.Runner) *Engine {
	return &Engine{store: dataStore, runner: commandRunner}
}

func (engine *Engine) Analyze(ctx context.Context, request Request) (*model.Analysis, error) {
	if request.Good == nil || request.Bad == nil {
		return nil, errors.New("good and bad captures are required")
	}
	if request.Repetitions <= 0 {
		request.Repetitions = 3
	}
	if request.MaxExperiments <= 0 {
		request.MaxExperiments = 128
	}
	if request.MaxFactors <= 0 {
		request.MaxFactors = 512
	}
	if request.MaxOutputBytes <= 0 {
		request.MaxOutputBytes = 8 << 20
	}
	if len(request.Command) == 0 {
		request.Command = append([]string(nil), request.Bad.Command.Arguments...)
	}

	factors, boundaries := Compare(request.Good, request.Bad, request.MaxFactors)
	analysis := &model.Analysis{
		SchemaVersion:      3,
		ID:                 id.New("ana"),
		CreatedAt:          time.Now().UTC(),
		GoodCaptureID:      request.Good.ID,
		BadCaptureID:       request.Bad.ID,
		Factors:            factors,
		Status:             model.StatusUnproven,
		EvidenceBoundaries: boundaries,
		Repetitions:        request.Repetitions,
		ExperimentBudget:   request.MaxExperiments,
	}

	goodBaseline, err := engine.verifyBaseline(ctx, request.Good, request.Command, true, request.Repetitions, request.MaxOutputBytes)
	analysis.Experiments = append(analysis.Experiments, goodBaseline...)
	if err != nil {
		analysis.Limitations = append(analysis.Limitations, "good baseline is not stable: "+err.Error())
		return engine.finish(analysis, nil)
	}
	badBaseline, err := engine.verifyBaseline(ctx, request.Bad, request.Command, false, request.Repetitions, request.MaxOutputBytes)
	analysis.Experiments = append(analysis.Experiments, badBaseline...)
	if err != nil {
		analysis.Limitations = append(analysis.Limitations, "bad baseline is not stable: "+err.Error())
		return engine.finish(analysis, nil)
	}
	if len(factors) == 0 {
		analysis.Limitations = append(analysis.Limitations, "no supported intervenable differences were found")
		return engine.finish(analysis, nil)
	}

	factorByID := make(map[string]model.Factor, len(factors))
	allIDs := make([]string, 0, len(factors))
	for _, factor := range factors {
		factorByID[factor.ID] = factor
		allIDs = append(allIDs, factor.ID)
	}
	experimentsRemaining := request.MaxExperiments - len(analysis.Experiments)
	if experimentsRemaining <= 0 {
		analysis.Limitations = append(analysis.Limitations, "experiment budget exhausted by baseline verification")
		return engine.finish(analysis, nil)
	}

	minimized, used, minimizeErr := DDMin(allIDs, experimentsRemaining, func(candidate []string) (bool, error) {
		records, passes, err := engine.runIntervention(ctx, request.Bad, request.Good, request.Command, candidate, factorByID, request.Repetitions, true, request.MaxOutputBytes)
		analysis.Experiments = append(analysis.Experiments, records...)
		return passes, err
	})
	_ = used
	if minimizeErr != nil {
		analysis.Limitations = append(analysis.Limitations, minimizeErr.Error())
		return engine.finish(analysis, nil)
	}
	if len(minimized) == 0 {
		analysis.Limitations = append(analysis.Limitations, "no factor set repaired the bad world")
		return engine.finish(analysis, nil)
	}

	if len(analysis.Experiments)+request.Repetitions > request.MaxExperiments {
		analysis.Limitations = append(analysis.Limitations, "experiment budget does not permit reverse verification")
		return engine.finish(analysis, nil)
	}
	reverseRecords, reversePasses, err := engine.runIntervention(ctx, request.Good, request.Bad, request.Command, minimized, factorByID, request.Repetitions, false, request.MaxOutputBytes)
	analysis.Experiments = append(analysis.Experiments, reverseRecords...)
	if err != nil {
		analysis.Limitations = append(analysis.Limitations, "reverse verification failed: "+err.Error())
		return engine.finish(analysis, nil)
	}
	if !reversePasses {
		analysis.Status = model.StatusSupported
		analysis.CausalFactors = minimized
		analysis.Limitations = append(analysis.Limitations, "bad-to-good intervention repaired the failure but reverse intervention did not reproduce it")
		return engine.finish(analysis, nil)
	}

	minimal, minimalityRecords, err := engine.verifyMinimality(ctx, request, minimized, factorByID)
	analysis.Experiments = append(analysis.Experiments, minimalityRecords...)
	if err != nil {
		analysis.Limitations = append(analysis.Limitations, "minimality verification incomplete: "+err.Error())
		analysis.Status = model.StatusSupported
		analysis.CausalFactors = minimized
		return engine.finish(analysis, nil)
	}
	if !minimal {
		analysis.Status = model.StatusSupported
		analysis.CausalFactors = minimized
		analysis.Limitations = append(analysis.Limitations, "strict subset also caused the intervention outcome")
		return engine.finish(analysis, nil)
	}

	analysis.Status = model.StatusProven
	analysis.CausalFactors = minimized
	analysis.ForwardVerified = true
	analysis.ReverseVerified = true
	analysis.MinimalInModel = true
	analysis.Summary = "smallest tested factor set repaired the bad world and reproduced the failure in the opposite direction"
	return engine.finish(analysis, nil)
}

func (engine *Engine) verifyBaseline(ctx context.Context, captureValue *model.Capture, command []string, expected bool, repetitions int, maxOutput int64) ([]model.Experiment, error) {
	records := make([]model.Experiment, 0, repetitions)
	for index := 0; index < repetitions; index++ {
		record, passed, err := engine.runWorld(ctx, captureValue, captureValue, command, nil, nil, maxOutput, "baseline")
		records = append(records, record)
		if err != nil {
			return records, err
		}
		if passed != expected {
			return records, fmt.Errorf("repetition %d expected oracle=%t got %t", index+1, expected, passed)
		}
	}
	return records, nil
}

func (engine *Engine) runIntervention(ctx context.Context, base, source *model.Capture, command, factorIDs []string, factors map[string]model.Factor, repetitions int, expected bool, maxOutput int64) ([]model.Experiment, bool, error) {
	selected := make([]model.Factor, 0, len(factorIDs))
	for _, identifier := range factorIDs {
		selected = append(selected, factors[identifier])
	}
	results := make([]model.Experiment, 0, repetitions)
	for index := 0; index < repetitions; index++ {
		record, passed, err := engine.runWorld(ctx, base, source, command, selected, factorIDs, maxOutput, "intervention")
		results = append(results, record)
		if err != nil {
			return results, false, err
		}
		if passed != expected {
			return results, false, nil
		}
	}
	return results, true, nil
}

func (engine *Engine) runWorld(ctx context.Context, base, source *model.Capture, command []string, selected []model.Factor, factorIDs []string, maxOutput int64, kind string) (model.Experiment, bool, error) {
	root, err := os.MkdirTemp("", "worldbisect-experiment-")
	if err != nil {
		return model.Experiment{}, false, err
	}
	defer os.RemoveAll(root)
	if err := workspace.Materialize(root, base.Workspace, engine.store); err != nil {
		return model.Experiment{}, false, err
	}
	environment := copyMap(base.Command.Environment)
	for _, factor := range selected {
		switch factor.Type {
		case model.FactorEnvironment:
			if source.ID == base.ID {
				continue
			}
			present, value := sourceEnvironment(factor, source == nil || source.ID == "")
			if source.ID == source.ID {
				if source.ID == base.ID {
					present, value = factor.BadPresent, factor.BadValue
				}
			}
			if base.ID == factorOwnerID(base, factor, true) {
				present, value = factor.GoodPresent, factor.GoodValue
			}
			if source.ID != base.ID {
				if source.ID == factorOwnerID(source, factor, true) {
					present, value = factor.GoodPresent, factor.GoodValue
				} else {
					present, value = factor.BadPresent, factor.BadValue
				}
			}
			if present {
				environment[factor.Key] = value
			} else {
				delete(environment, factor.Key)
			}
		case model.FactorWorkspace:
			entry, present := factor.BadEntry, factor.BadPresent
			if source.ID == factorOwnerID(source, factor, true) {
				entry, present = factor.GoodEntry, factor.GoodPresent
			}
			if err := workspace.Apply(root, factor.Key, entry, present, engine.store); err != nil {
				return model.Experiment{}, false, err
			}
		}
	}
	start := time.Now().UTC()
	result, runErr := engine.runner.Run(ctx, runner.Request{
		Command:        command,
		Directory:      root,
		Environment:    model.EnvironmentToList(environment),
		Timeout:        time.Duration(base.Command.TimeoutMS) * time.Millisecond,
		MaxOutputBytes: maxOutput,
		Trace:          false,
	})
	record := model.Experiment{
		ID:              id.New("exp"),
		Kind:            kind,
		StartedAt:       start,
		FinishedAt:      time.Now().UTC(),
		BaseCaptureID:   base.ID,
		SourceCaptureID: source.ID,
		FactorIDs:       append([]string(nil), factorIDs...),
	}
	if result != nil {
		record.Result = *result
		record.OracleResult = oracle.Evaluate(base.Oracle, *result, root)
	}
	if runErr != nil {
		record.Error = runErr.Error()
	}
	return record, record.OracleResult.Passed, runErr
}

func (engine *Engine) verifyMinimality(ctx context.Context, request Request, minimized []string, factors map[string]model.Factor) (bool, []model.Experiment, error) {
	var records []model.Experiment
	for index := range minimized {
		if len(records)+len(request.Good.Command.Arguments) >= request.MaxExperiments {
			return false, records, errors.New("experiment budget exhausted")
		}
		subset := append([]string(nil), minimized[:index]...)
		subset = append(subset, minimized[index+1:]...)
		if len(subset) == 0 {
			continue
		}
		trial, passes, err := engine.runIntervention(ctx, request.Bad, request.Good, request.Command, subset, factors, request.Repetitions, true, request.MaxOutputBytes)
		records = append(records, trial...)
		if err != nil {
			return false, records, err
		}
		if passes {
			return false, records, nil
		}
	}
	return true, records, nil
}

func (engine *Engine) finish(analysis *model.Analysis, err error) (*model.Analysis, error) {
	analysis.Normalize()
	if saveErr := engine.store.SaveAnalysis(analysis); saveErr != nil {
		return nil, saveErr
	}
	if auditErr := engine.store.AppendAudit("analysis", "analysis", analysis.ID, map[string]any{
		"status":              analysis.Status,
		"good_capture_id":     analysis.GoodCaptureID,
		"bad_capture_id":      analysis.BadCaptureID,
		"causal_factor_count": len(analysis.CausalFactors),
	}); auditErr != nil {
		return nil, auditErr
	}
	return analysis, err
}

func copyMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func sourceEnvironment(factor model.Factor, _ bool) (bool, string) {
	return factor.GoodPresent, factor.GoodValue
}

func factorOwnerID(captureValue *model.Capture, factor model.Factor, good bool) string {
	if good {
		return captureValue.ID
	}
	return ""
}

func sortedFactors(values []model.Factor) []model.Factor {
	result := append([]model.Factor(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
