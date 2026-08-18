package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func Parse(value string) (model.Oracle, error) {
	if value == "timeout" {
		return model.Oracle{Kind: "timeout"}, nil
	}
	kind, content, found := strings.Cut(value, "=")
	if !found {
		return model.Oracle{}, errors.New("oracle must be kind=value or timeout")
	}
	switch kind {
	case "exit":
		code, err := strconv.Atoi(content)
		if err != nil {
			return model.Oracle{}, err
		}
		return model.Oracle{Kind: kind, ExpectedExitCode: &code}, nil
	case "stdout_regex", "stderr_regex":
		if _, err := regexp.Compile(content); err != nil {
			return model.Oracle{}, err
		}
		return model.Oracle{Kind: kind, Pattern: content}, nil
	case "file_digest":
		path, digest, found := strings.Cut(content, ":sha256:")
		if !found || path == "" || len(digest) != 64 {
			return model.Oracle{}, errors.New("file_digest requires <relative-path>:sha256:<hex>")
		}
		return model.Oracle{Kind: kind, File: filepath.ToSlash(path), Digest: digest}, nil
	default:
		return model.Oracle{}, fmt.Errorf("unsupported oracle %q", kind)
	}
}

func Evaluate(spec model.Oracle, result model.ProcessResult, workspace string) model.OracleResult {
	switch spec.Kind {
	case "exit":
		if spec.ExpectedExitCode == nil {
			return model.OracleResult{Passed: false, Detail: "expected exit code missing"}
		}
		passed := !result.TimedOut && result.ExitCode == *spec.ExpectedExitCode
		return model.OracleResult{Passed: passed, Detail: fmt.Sprintf("exit=%d expected=%d", result.ExitCode, *spec.ExpectedExitCode)}
	case "timeout":
		return model.OracleResult{Passed: result.TimedOut, Detail: fmt.Sprintf("timed_out=%t", result.TimedOut)}
	case "stdout_regex":
		matched, err := regexp.MatchString(spec.Pattern, result.Stdout)
		if err != nil {
			return model.OracleResult{Passed: false, Detail: err.Error()}
		}
		return model.OracleResult{Passed: matched, Detail: "stdout regex evaluated"}
	case "stderr_regex":
		matched, err := regexp.MatchString(spec.Pattern, result.Stderr)
		if err != nil {
			return model.OracleResult{Passed: false, Detail: err.Error()}
		}
		return model.OracleResult{Passed: matched, Detail: "stderr regex evaluated"}
	case "file_digest":
		path, err := safeWorkspacePath(workspace, spec.File)
		if err != nil {
			return model.OracleResult{Passed: false, Detail: err.Error()}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return model.OracleResult{Passed: false, Detail: err.Error()}
		}
		sum := sha256.Sum256(content)
		actual := hex.EncodeToString(sum[:])
		return model.OracleResult{Passed: actual == spec.Digest, Detail: "file sha256=" + actual}
	default:
		return model.OracleResult{Passed: false, Detail: "unsupported oracle"}
	}
}

func safeWorkspacePath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("oracle file must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("oracle file escapes workspace")
	}
	return filepath.Join(root, clean), nil
}
