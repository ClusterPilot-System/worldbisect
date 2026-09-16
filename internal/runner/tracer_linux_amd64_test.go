//go:build linux && amd64

package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func requireNativeTracer(t *testing.T) {
	t.Helper()
	if raceEnabled || !nativeTracerAvailable() {
		t.Skip("native tracing disabled on this runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.Command("/bin/true")
	command.SysProcAttr = &syscall.SysProcAttr{}
	_, _, err := runTraced(ctx, command)
	if errors.Is(err, syscall.EPERM) && os.Getenv("WORLDBISECT_REQUIRE_NATIVE_TRACE") != "1" {
		t.Skip("kernel denies ptrace")
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestNativeTracerJoinsNestedChildrenAndOutput(t *testing.T) {
	requireNativeTracer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.Command("/bin/sh", "-c", "i=0; while [ $i -lt 20 ]; do /bin/sh -c 'cat /dev/null; printf child; printf error >&2' & i=$((i+1)); done; wait")
	command.SysProcAttr = &syscall.SysProcAttr{}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	paths, _, err := runTraced(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != strings.Repeat("child", 20) || stderr.String() != strings.Repeat("error", 20) {
		t.Fatalf("incomplete output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	found := false
	for _, path := range paths {
		found = found || path == "/dev/null"
	}
	if !found {
		t.Fatal("nested child file access was not traced")
	}
}

func TestNativeTracersDoNotReapOtherCommands(t *testing.T) {
	requireNativeTracer(t)
	var workers sync.WaitGroup
	errorsFound := make(chan error, 12)
	for worker := 0; worker < 12; worker++ {
		workers.Add(1)
		go func(traced bool) {
			defer workers.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.Command("/bin/sh", "-c", "cat /dev/null; sleep 0.02; printf complete")
			command.SysProcAttr = &syscall.SysProcAttr{}
			var stdout bytes.Buffer
			command.Stdout = &stdout
			var err error
			if traced {
				_, _, err = runTraced(ctx, command)
			} else {
				err = startAndWait(ctx, command)
			}
			if err != nil || stdout.String() != "complete" {
				errorsFound <- fmt.Errorf("traced=%v output=%q err=%v", traced, stdout.String(), err)
			}
		}(worker%2 == 0)
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
}

func TestNativeTracerCancelsNestedChildren(t *testing.T) {
	requireNativeTracer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	command := exec.Command("/bin/sh", "-c", "sleep 30 & sleep 30 & wait")
	command.SysProcAttr = &syscall.SysProcAttr{}
	var stdout bytes.Buffer
	command.Stdout = &stdout
	started := time.Now()
	_, _, err := runTraced(ctx, command)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("cancellation was not bounded: err=%v duration=%s", err, time.Since(started))
	}
}
