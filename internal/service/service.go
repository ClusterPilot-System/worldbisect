package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/capture"
	"github.com/ClusterPilot-System/worldbisect/internal/config"
	"github.com/ClusterPilot-System/worldbisect/internal/experiment"
	"github.com/ClusterPilot-System/worldbisect/internal/jobs"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

type Service struct {
	cfg    config.Config
	store  *store.Store
	runner *runner.Runner
	jobs   *jobs.Manager
}

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	Details    map[string]any
}

func (err *Error) Error() string { return err.Message }

func New(cfg config.Config, dataStore *store.Store, commandRunner *runner.Runner) *Service {
	manager := jobs.New(dataStore, cfg.Workers, cfg.LeaseDuration(), cfg.MaxAttempts)
	service := &Service{cfg: cfg, store: dataStore, runner: commandRunner, jobs: manager}
	manager.Register("capture", service.handleCapture)
	manager.Register("analysis", service.handleAnalysis)
	return service
}

func (service *Service) Run(ctx context.Context) {
	service.jobs.Run(ctx)
}

func (service *Service) EnqueueCapture(ctx context.Context, principal, fingerprint, key string, request model.CaptureJobRequest) (*model.Job, bool, error) {
	if !service.cfg.RemoteExecutionEnabled {
		return nil, false, forbidden("remote_execution_disabled", "remote execution is disabled")
	}
	if err := validateCaptureRequest(request); err != nil {
		return nil, false, badRequest(err.Error())
	}
	binding, err := service.authorizeExecution(request.Command)
	if err != nil {
		return nil, false, forbidden("command_not_allowed", err.Error())
	}
	binding.ExecutableFile.Close()
	binding.DirectoryFile.Close()
	payload, digest, err := canonicalPayload(request)
	if err != nil {
		return nil, false, err
	}
	job, existing, err := service.jobs.Enqueue("capture", payload, principal, fingerprint, key, digest)
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyConflict) {
			return nil, false, conflict("idempotency_conflict", "idempotency key was already used with a different request")
		}
		return nil, false, err
	}
	return job, existing, nil
}

func (service *Service) EnqueueAnalysis(ctx context.Context, principal, fingerprint, key string, request model.AnalysisJobRequest) (*model.Job, bool, error) {
	if request.GoodCaptureID == "" || request.BadCaptureID == "" {
		return nil, false, badRequest("good_capture_id and bad_capture_id are required")
	}
	payload, digest, err := canonicalPayload(request)
	if err != nil {
		return nil, false, err
	}
	job, existing, err := service.jobs.Enqueue("analysis", payload, principal, fingerprint, key, digest)
	if err != nil {
		if errors.Is(err, store.ErrIdempotencyConflict) {
			return nil, false, conflict("idempotency_conflict", "idempotency key was already used with a different request")
		}
		return nil, false, err
	}
	return job, existing, nil
}

func (service *Service) handleCapture(ctx context.Context, job *model.Job) (string, error) {
	var request model.CaptureJobRequest
	if err := json.Unmarshal(job.Payload, &request); err != nil {
		return "", err
	}
	binding, err := service.authorizeExecution(request.Command)
	if err != nil {
		return "", err
	}
	defer binding.ExecutableFile.Close()
	defer binding.DirectoryFile.Close()
	capturer := capture.New(service.store, service.runner)
	record, err := capturer.CaptureWithBinding(ctx, capture.Request{
		Label:     request.Label,
		Workspace: request.Command.Directory,
		Command:   request.Command,
		Oracle:    request.Oracle,
		Limits: model.CaptureLimits{
			MaxWorkspaceFiles: service.cfg.Quotas.MaxWorkspaceFiles,
			MaxWorkspaceBytes: service.cfg.Quotas.MaxWorkspaceBytes,
			MaxOutputBytes:    service.cfg.Quotas.MaxOutputBytes,
			Timeout:           time.Duration(request.Command.TimeoutMS) * time.Millisecond,
		},
	}, binding)
	if err != nil && record == nil {
		return "", err
	}
	return record.ID, err
}

func (service *Service) handleAnalysis(ctx context.Context, job *model.Job) (string, error) {
	var request model.AnalysisJobRequest
	if err := json.Unmarshal(job.Payload, &request); err != nil {
		return "", err
	}
	good, err := service.store.LoadCapture(request.GoodCaptureID)
	if err != nil {
		return "", err
	}
	bad, err := service.store.LoadCapture(request.BadCaptureID)
	if err != nil {
		return "", err
	}
	analysis, err := experiment.New(service.store, service.runner).Analyze(ctx, experiment.Request{
		Good:           good,
		Bad:            bad,
		Command:        bad.Command.Arguments,
		Repetitions:    request.Repetitions,
		MaxFactors:     service.cfg.Quotas.MaxFactors,
		MaxExperiments: service.cfg.Quotas.MaxExperiments,
		MaxOutputBytes: service.cfg.Quotas.MaxOutputBytes,
	})
	if analysis == nil {
		return "", err
	}
	return analysis.ID, err
}

func (service *Service) authorizeExecution(command model.CommandSpec) (*runner.ExecutionBinding, error) {
	if len(command.Arguments) == 0 {
		return nil, errors.New("command is required")
	}
	requested := command.Arguments[0]
	if !filepath.IsAbs(requested) {
		return nil, errors.New("remote command must be an absolute path")
	}
	cleanRequested := filepath.Clean(requested)
	authorizedExecutable := ""
	for _, commandPath := range service.cfg.AllowedCommands {
		if cleanRequested == commandPath {
			// Open the configured allowlist entry, never the caller's path.
			authorizedExecutable = commandPath
			break
		}
	}
	if authorizedExecutable == "" {
		return nil, fmt.Errorf("command %q is not allowed", requested)
	}
	if command.Directory == "" || !filepath.IsAbs(command.Directory) {
		return nil, errors.New("working directory must be an absolute path")
	}
	resolvedDirectory, err := filepath.EvalSymlinks(command.Directory)
	if err != nil {
		return nil, err
	}
	resolvedDirectory, err = filepath.Abs(resolvedDirectory)
	if err != nil {
		return nil, err
	}
	allowedRoot, relativeDirectory := "", ""
	for _, root := range service.cfg.AllowedWorkingDirectories {
		relative, err := filepath.Rel(root, resolvedDirectory)
		if err == nil && filepath.IsLocal(relative) {
			allowedRoot, relativeDirectory = root, relative
			break
		}
	}
	if allowedRoot == "" {
		return nil, fmt.Errorf("working directory %q is not allowed", command.Directory)
	}

	executableFile, err := openExecutable(authorizedExecutable)
	if err != nil {
		return nil, err
	}
	executablePath, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", executableFile.Fd()))
	if err != nil {
		executableFile.Close()
		return nil, err
	}
	if executablePath != authorizedExecutable {
		executableFile.Close()
		return nil, errors.New("opened executable path does not match authorized path")
	}
	identity, err := runner.Identity(executableFile)
	if err != nil {
		executableFile.Close()
		return nil, err
	}
	directoryFile, err := openDirectoryBeneath(allowedRoot, relativeDirectory)
	if err != nil {
		executableFile.Close()
		return nil, err
	}
	directoryPath, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", directoryFile.Fd()))
	if err != nil {
		executableFile.Close()
		directoryFile.Close()
		return nil, err
	}
	if directoryPath != resolvedDirectory {
		executableFile.Close()
		directoryFile.Close()
		return nil, errors.New("opened working directory does not match authorized path")
	}
	return &runner.ExecutionBinding{
		ExecutableFile: executableFile,
		DirectoryFile:  directoryFile,
		ExecutablePath: authorizedExecutable,
		DirectoryPath:  resolvedDirectory,
		Identity:       identity,
	}, nil
}

func openExecutable(configuredPath string) (*os.File, error) {
	file, err := os.OpenFile(configuredPath, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		file.Close()
		return nil, errors.New("opened command must be a regular executable file")
	}
	return file, nil
}

func openDirectoryBeneath(configuredRoot, relative string) (*os.File, error) {
	if !filepath.IsLocal(relative) {
		return nil, errors.New("working directory escapes its configured root")
	}
	// OpenRoot checks the file type after opening on Go 1.25. Require a
	// directory in the initial syscall as well, so a replaced root cannot
	// block authorization on a FIFO before the type check runs.
	anchor, err := os.OpenFile(configuredRoot, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer anchor.Close()
	anchorFD := fmt.Sprintf("/proc/self/fd/%d", anchor.Fd())
	anchorPath, err := os.Readlink(anchorFD)
	if err != nil {
		return nil, err
	}
	if anchorPath != configuredRoot {
		return nil, errors.New("opened working directory root does not match configured path")
	}
	root, err := os.OpenRoot(anchorFD)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Root constrains traversal during the open, including replacements after
	// canonicalization. O_DIRECTORY rejects special files before opening them.
	return root.OpenFile(relative, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
}

func validateCaptureRequest(request model.CaptureJobRequest) error {
	if len(request.Command.Arguments) == 0 {
		return errors.New("command is required")
	}
	if request.Command.TimeoutMS <= 0 || request.Command.TimeoutMS > time.Hour.Milliseconds() {
		return errors.New("timeout_ms must be between 1 and 3600000")
	}
	if request.Oracle.Kind == "" {
		return errors.New("oracle is required")
	}
	return nil
}

func canonicalPayload(value any) ([]byte, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(payload)
	return payload, hex.EncodeToString(sum[:]), nil
}

func badRequest(message string) *Error {
	return &Error{Code: "bad_request", Message: message, HTTPStatus: 400}
}

func forbidden(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 403}
}

func conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 409}
}
