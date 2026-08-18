package version

import (
	"fmt"
	"runtime"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func String() string {
	return fmt.Sprintf("worldbisect %s (%s, %s, %s/%s)", Version, Commit, BuildTime, runtime.GOOS, runtime.GOARCH)
}

func Info() map[string]string {
	return map[string]string{"version": Version, "commit": Commit, "build_time": BuildTime, "go": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH}
}
