package capture

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/id"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/observe"
	"github.com/ClusterPilot-System/worldbisect/internal/oracle"
	"github.com/ClusterPilot-System/worldbisect/internal/redact"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
	"github.com/ClusterPilot-System/worldbisect/internal/workspace"
)

type Request struct {
	Label     string
	Workspace string
	Command   model.CommandSpec
	Oracle    model.Oracle
	Limits    model.CaptureLimits
}

type Capturer struct {
	store  *store.Store
	runner *runner.Runner
}

func New(dataStore *store.Store, commandRunner *runner.Runner) *Capturer {
	return &Capturer{store: dataStore, runner: commandRunner}
}

func (capturer *Capturer) Capture(ctx context.Context, request Request) (*model.Capture, error) {
	limits := request.Limits.WithDefaults()
	root, err := workspace.CanonicalRoot(request.Workspace)
	if err != nil {
		return nil, err
	}
	if len(request.Command.Arguments) == 0 {
		return nil, errors.New("command is required")
	}
	if request.Command.Directory == "" {
		request.Command.Directory = root
	}
	if request.Command.TimeoutMS <= 0 {
		request.Command.TimeoutMS = limits.Timeout.Milliseconds()
	}

	secretKey, err := loadOrCreateRedactionKey(capturer.store.Root())
	if err != nil {
		return nil, err
	}
	sanitizedEnvironment, secretEvidence := redact.Environment(request.Command.Environment, secretKey)
	request.Command.Environment = sanitizedEnvironment

	before, err := workspace.Scan(root, capturer.store, limits.MaxWorkspaceFiles, limits.MaxWorkspaceBytes)
	if err != nil {
		return nil, err
	}
	hostEvidence := observe.Host(root)
	started := time.Now().UTC()
	result, runErr := capturer.runner.Run(ctx, runner.Request{
		Command:        request.Command.Arguments,
		Directory:      request.Command.Directory,
		Environment:    model.EnvironmentToList(request.Command.Environment),
		Timeout:        time.Duration(request.Command.TimeoutMS) * time.Millisecond,
		MaxOutputBytes: limits.MaxOutputBytes,
		Trace:          true,
	})
	finished := time.Now().UTC()
	after, scanErr := workspace.Scan(root, capturer.store, limits.MaxWorkspaceFiles, limits.MaxWorkspaceBytes)
	if scanErr != nil {
		return nil, scanErr
	}
	captureValue := &model.Capture{
		SchemaVersion:  3,
		ID:             id.New("cap"),
		CreatedAt:      finished,
		StartedAt:      started,
		FinishedAt:     finished,
		Label:          request.Label,
		WorkspaceRoot:  root,
		Command:        request.Command,
		Oracle:         request.Oracle,
		Before:         before,
		Workspace:      after,
		Host:           hostEvidence,
		SecretEvidence: secretEvidence,
	}
	if result != nil {
		captureValue.Result = *result
		captureValue.OracleResult = oracle.Evaluate(request.Oracle, *result, root)
		captureValue.ConsultedPaths = normalizeConsulted(root, result.ConsultedPaths)
		captureValue.EvidenceBoundaries = append(captureValue.EvidenceBoundaries, result.Boundaries...)
	}
	if runErr != nil {
		captureValue.Warnings = append(captureValue.Warnings, runErr.Error())
	}
	captureValue.EvidenceBoundaries = append(captureValue.EvidenceBoundaries,
		"host files, packages, libraries, mounts, resources, network, secrets, schedules, hardware, and kernel internals are evidence-only unless independently controlled",
	)
	captureValue.Normalize()
	if err := capturer.store.SaveCapture(captureValue); err != nil {
		return nil, err
	}
	if err := capturer.store.AppendAudit("capture", "capture", captureValue.ID, map[string]any{
		"command_digest": model.DigestStrings(request.Command.Arguments),
		"workspace":      root,
		"oracle_passed":  captureValue.OracleResult.Passed,
	}); err != nil {
		return nil, err
	}
	if runErr != nil {
		return captureValue, runErr
	}
	return captureValue, nil
}

func normalizeConsulted(root string, paths []string) []model.ConsultedPath {
	seen := make(map[string]bool)
	result := make([]model.ConsultedPath, 0, len(paths))
	for _, value := range paths {
		if value == "" {
			continue
		}
		absolute := value
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(root, absolute)
		}
		absolute = filepath.Clean(absolute)
		if seen[absolute] {
			continue
		}
		seen[absolute] = true
		relative, err := filepath.Rel(root, absolute)
		inside := err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
		item := model.ConsultedPath{Path: absolute, InWorkspace: inside}
		if inside {
			item.Relative = filepath.ToSlash(relative)
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func loadOrCreateRedactionKey(root string) ([]byte, error) {
	path := filepath.Join(root, "keys", "redaction_hmac.key")
	if content, err := os.ReadFile(path); err == nil {
		if len(content) < 32 {
			return nil, errors.New("redaction key is too short")
		}
		return content, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ReadFile(path)
		}
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func FingerprintSecret(key []byte, value string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

func ValidateCapture(captureValue *model.Capture) error {
	if captureValue.ID == "" || captureValue.SchemaVersion <= 0 {
		return fmt.Errorf("invalid capture identity")
	}
	return nil
}
