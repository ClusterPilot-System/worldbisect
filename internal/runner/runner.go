package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

type Request struct {
	Command        []string
	Directory      string
	Environment    []string
	Timeout        time.Duration
	MaxOutputBytes int64
	Trace          bool
	Executable     *ExecutionBinding
}

type ExecutionBinding struct {
	ExecutableFile *os.File
	DirectoryFile  *os.File
	ExecutablePath string
	DirectoryPath  string
	Identity       FileIdentity
}

type FileIdentity struct {
	Device    uint64
	Inode     uint64
	Size      int64
	Mode      uint32
	UID       uint32
	GID       uint32
	ModTimeNS int64
	ChangeNS  int64
	Digest    string
}

type Runner struct{}

func New() *Runner { return &Runner{} }

func (runner *Runner) Run(ctx context.Context, request Request) (*model.ProcessResult, error) {
	if len(request.Command) == 0 {
		return nil, errors.New("command is required")
	}
	if request.Timeout <= 0 {
		request.Timeout = 2 * time.Minute
	}
	if request.MaxOutputBytes <= 0 {
		request.MaxOutputBytes = 8 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()

	commandPath := request.Command[0]
	directory := request.Directory
	if request.Executable != nil {
		if err := validateBinding(request.Executable); err != nil {
			return nil, err
		}
		// ExtraFiles gives the executable a non-CLOEXEC descriptor 3 for
		// interpreter-backed scripts. The original directory descriptor remains
		// available while the child performs chdir before exec.
		commandPath = "/proc/self/fd/3"
		directory = fmt.Sprintf("/proc/self/fd/%d", request.Executable.DirectoryFile.Fd())
	}

	command := exec.Command(commandPath, request.Command[1:]...)
	command.Dir = directory
	command.Env = request.Environment
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if request.Executable != nil {
		command.ExtraFiles = []*os.File{request.Executable.ExecutableFile, request.Executable.DirectoryFile}
	}

	stdout := newLimitedBuffer(request.MaxOutputBytes)
	stderr := newLimitedBuffer(request.MaxOutputBytes)
	command.Stdout = stdout
	command.Stderr = stderr
	started := time.Now().UTC()

	var consulted []string
	var boundaries []string
	var runErr error
	if request.Trace && nativeTracerAvailable() && !raceEnabled {
		consulted, boundaries, runErr = runTraced(ctx, command)
	} else {
		if request.Trace {
			boundaries = append(boundaries, "native syscall tracing unavailable; basic capture used")
		}
		runErr = startAndWait(ctx, command)
	}
	finished := time.Now().UTC()
	result := &model.ProcessResult{
		ExitCode:        exitCode(runErr),
		Signal:          signalName(runErr),
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded),
		StartedAt:       started,
		FinishedAt:      finished,
		DurationMS:      finished.Sub(started).Milliseconds(),
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		OutputTruncated: stdout.Truncated() || stderr.Truncated(),
		ConsultedPaths:  uniqueSorted(consulted),
		Boundaries:      uniqueSorted(boundaries),
	}
	if result.TimedOut {
		return result, context.DeadlineExceeded
	}
	if runErr != nil {
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			return result, runErr
		}
		return result, runErr
	}
	return result, nil
}

func startAndWait(ctx context.Context, command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		<-done
		return ctx.Err()
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var code interface{ ExitCode() int }
	if errors.As(err, &code) {
		return code.ExitCode()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func signalName(err error) string {
	var signal interface{ Signal() string }
	if errors.As(err, &signal) {
		return signal.Signal()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return status.Signal().String()
		}
	}
	return ""
}

type tracedExitError struct {
	code   int
	signal string
}

func (err tracedExitError) Error() string {
	if err.signal != "" {
		return "process terminated by " + err.signal
	}
	return fmt.Sprintf("process exited with code %d", err.code)
}

func (err tracedExitError) ExitCode() int {
	return err.code
}

func (err tracedExitError) Signal() string {
	return err.signal
}

type limitedBuffer struct {
	mutex     sync.Mutex
	buffer    bytes.Buffer
	remaining int64
	truncated bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{remaining: limit}
}

func (buffer *limitedBuffer) Write(content []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	original := len(content)
	if buffer.remaining <= 0 {
		buffer.truncated = true
		return original, nil
	}
	if int64(len(content)) > buffer.remaining {
		content = content[:buffer.remaining]
		buffer.truncated = true
	}
	_, _ = buffer.buffer.Write(content)
	buffer.remaining -= int64(len(content))
	return original, nil
}

func (buffer *limitedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}

func (buffer *limitedBuffer) Truncated() bool {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.truncated
}

func validateBinding(binding *ExecutionBinding) error {
	if binding.ExecutableFile == nil || binding.DirectoryFile == nil {
		return errors.New("execution binding is incomplete")
	}
	identity, err := Identity(binding.ExecutableFile)
	if err != nil {
		return err
	}
	if identity != binding.Identity {
		return errors.New("authorized executable changed before execution")
	}
	resolvedExecutable, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", binding.ExecutableFile.Fd()))
	if err != nil {
		return err
	}
	resolvedDirectory, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", binding.DirectoryFile.Fd()))
	if err != nil {
		return err
	}
	if resolvedExecutable != binding.ExecutablePath || resolvedDirectory != binding.DirectoryPath {
		return errors.New("authorized execution path changed")
	}
	return nil
}

func Identity(file *os.File) (FileIdentity, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return FileIdentity{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return FileIdentity{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return FileIdentity{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return FileIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileIdentity{}, errors.New("unsupported file identity")
	}
	return FileIdentity{
		Device:    uint64(stat.Dev),
		Inode:     stat.Ino,
		Size:      info.Size(),
		Mode:      uint32(info.Mode()),
		UID:       stat.Uid,
		GID:       stat.Gid,
		ModTimeNS: info.ModTime().UnixNano(),
		ChangeNS:  stat.Ctim.Sec*1e9 + stat.Ctim.Nsec,
		Digest:    hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func uniqueSorted(values []string) []string {
	set := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

var _ = runtime.GOOS
var _ = strings.Builder{}
