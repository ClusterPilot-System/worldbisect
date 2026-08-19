package experiment

import (
	"fmt"
	"sort"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func Compare(good, bad *model.Capture, maxFactors int) ([]model.Factor, []string) {
	var factors []model.Factor
	var boundaries []string

	allEnvironment := make(map[string]bool)
	for key := range good.Command.Environment {
		allEnvironment[key] = true
	}
	for key := range bad.Command.Environment {
		allEnvironment[key] = true
	}
	keys := sortedKeys(allEnvironment)
	for _, key := range keys {
		goodValue, goodExists := good.Command.Environment[key]
		badValue, badExists := bad.Command.Environment[key]
		if goodValue == badValue && goodExists == badExists {
			continue
		}
		if model.IsRedactedValue(goodValue) || model.IsRedactedValue(badValue) || secretKey(good, key) || secretKey(bad, key) {
			boundaries = append(boundaries, "secret environment variable "+key+" differs but is not eligible for intervention")
			continue
		}
		factors = append(factors, model.Factor{
			ID:           model.FactorID("env", key),
			Type:         model.FactorEnvironment,
			Key:          key,
			GoodPresent:  goodExists,
			BadPresent:   badExists,
			GoodValue:    goodValue,
			BadValue:     badValue,
			Intervenable: true,
		})
	}

	goodEntries := good.Workspace.EntryMap()
	badEntries := bad.Workspace.EntryMap()
	allPaths := make(map[string]bool)
	for path := range goodEntries {
		allPaths[path] = true
	}
	for path := range badEntries {
		allPaths[path] = true
	}
	paths := sortedKeys(allPaths)
	for _, path := range paths {
		goodEntry, goodExists := goodEntries[path]
		badEntry, badExists := badEntries[path]
		if goodExists && badExists && goodEntry.Equal(badEntry) {
			continue
		}
		if (goodExists && !goodEntry.Supported()) || (badExists && !badEntry.Supported()) {
			boundaries = append(boundaries, "unsupported workspace entry differs: "+path)
			continue
		}
		factors = append(factors, model.Factor{
			ID:           model.FactorID("workspace", path),
			Type:         model.FactorWorkspace,
			Key:          path,
			GoodPresent:  goodExists,
			BadPresent:   badExists,
			GoodEntry:    goodEntry,
			BadEntry:     badEntry,
			Intervenable: true,
		})
	}

	if good.Host.Digest() != bad.Host.Digest() {
		boundaries = append(boundaries, "host, mount, resource, or kernel evidence differs and is not an automatic causal factor in 1.0")
	}
	if len(good.ConsultedPaths) == 0 || len(bad.ConsultedPaths) == 0 {
		boundaries = append(boundaries, "consulted-path capture is incomplete; candidate scope is bounded to environment and workspace")
	}

	sort.Slice(factors, func(i, j int) bool { return factors[i].ID < factors[j].ID })
	if maxFactors > 0 && len(factors) > maxFactors {
		boundaries = append(boundaries, fmt.Sprintf("factor limit reached: %d of %d retained", maxFactors, len(factors)))
		factors = factors[:maxFactors]
	}
	return factors, uniqueSorted(boundaries)
}

func secretKey(captureValue *model.Capture, key string) bool {
	for _, evidence := range captureValue.SecretEvidence {
		if evidence.Name == key {
			return true
		}
	}
	return false
}

func sortedKeys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func uniqueSorted(values []string) []string {
	set := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	result := sortedKeys(set)
	return result
}
