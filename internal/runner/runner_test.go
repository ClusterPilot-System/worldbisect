package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunAndOutputLimit(t *testing.T) {
	result, err := New().Run(context.Background(), Request{
		Command:        []string{"/bin/sh", "-c", "printf 1234567890"},
		Timeout:        time.Second,
		MaxOutputBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "1234" || !result.OutputTruncated {
		t.Fatalf("result = %+v", result)
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	result, err := New().Run(context.Background(), Request{
		Command: []string{"/bin/sh", "-c", "sleep 10 & wait"},
		Timeout: 50 * time.Millisecond,
	})
	if err == nil || !result.TimedOut {
		t.Fatalf("expected timeout: result=%+v err=%v", result, err)
	}
}

func TestTracedTimeoutDoesNotBlockOnWait(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native tracer is only available on Linux AMD64")
	}
	result, err := New().Run(context.Background(), Request{
		Command:        []string{"/bin/sh", "-c", "while :; do :; done"},
		Timeout:        50 * time.Millisecond,
		MaxOutputBytes: 1024,
		Trace:          true,
	})
	for _, boundary := range result.Boundaries {
		if boundary == "ptrace options unavailable" {
			t.Skip("ptrace is unavailable in this Linux environment")
		}
	}
	if err == nil || !result.TimedOut {
		t.Fatalf("expected traced timeout: result=%+v err=%v", result, err)
	}
}

func TestBindingRejectsModifiedExecutable(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "tool")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	directoryFile, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directoryFile.Close()
	executableFile, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer executableFile.Close()
	identity, err := Identity(executableFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = New().Run(context.Background(), Request{
		Command:    []string{executable},
		Timeout:    time.Second,
		Executable: &ExecutionBinding{ExecutableFile: executableFile, DirectoryFile: directoryFile, ExecutablePath: executable, DirectoryPath: root, Identity: identity},
	})
	if err == nil {
		t.Fatal("modified executable accepted")
	}
}
