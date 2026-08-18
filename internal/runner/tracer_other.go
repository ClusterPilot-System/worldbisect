//go:build !linux || !amd64

package runner

import (
	"context"
	"os/exec"
)

func nativeTracerAvailable() bool { return false }

func runTraced(ctx context.Context, command *exec.Cmd) ([]string, []string, error) {
	return nil, []string{"native tracing unsupported on this platform"}, startAndWait(ctx, command)
}
