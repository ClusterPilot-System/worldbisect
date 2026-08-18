package observe

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/ClusterPilot-System/worldbisect/internal/model"
)

func Host(_ string) model.HostEvidence {
	hostname, _ := os.Hostname()
	groups := []int{}
	if values, err := os.Getgroups(); err == nil {
		groups = values
		sort.Ints(groups)
	}
	status := readKeyValues("/proc/self/status")
	kernel := ""
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err == nil {
		kernel = byteArrayString(uname.Release[:])
	}
	return model.HostEvidence{
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Kernel:         kernel,
		Hostname:       hostname,
		UID:            os.Getuid(),
		GID:            os.Getgid(),
		Groups:         groups,
		Capabilities:   status["CapEff"] + ":" + status["CapBnd"] + ":" + status["NoNewPrivs"],
		Seccomp:        status["Seccomp"] + ":" + status["Seccomp_filters"],
		Cgroups:        digestFile("/proc/self/cgroup"),
		MountDigest:    digestFile("/proc/self/mountinfo"),
		ResourceDigest: resourceDigest(),
		SecurityDigest: securityDigest(status),
	}
}

func readKeyValues(path string) map[string]string {
	result := make(map[string]string)
	file, err := os.Open(path)
	if err != nil {
		return result
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if found {
			result[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return result
}

func digestFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return "unavailable"
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func resourceDigest() string {
	values := []string{}
	for _, name := range []string{"RLIMIT_NOFILE", "RLIMIT_NPROC", "RLIMIT_AS"} {
		values = append(values, name)
	}
	values = append(values, strconv.Itoa(runtime.GOMAXPROCS(0)))
	return model.DigestStrings(values)
}

func securityDigest(status map[string]string) string {
	values := []string{status["Uid"], status["Gid"], status["Groups"], status["CapInh"], status["CapPrm"], status["CapEff"], status["CapBnd"], status["Seccomp"], status["NoNewPrivs"]}
	return model.DigestStrings(values)
}

func byteArrayString(values []int8) string {
	bytes := make([]byte, 0, len(values))
	for _, value := range values {
		if value == 0 {
			break
		}
		bytes = append(bytes, byte(value))
	}
	return string(bytes)
}

var _ = user.Current
