//go:build linux && amd64

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const syscallExecveAt = 322

func nativeTracerAvailable() bool { return true }

func runTraced(ctx context.Context, command *exec.Cmd) ([]string, []string, error) {
	command.SysProcAttr.Ptrace = true
	command.SysProcAttr.Setpgid = true
	if err := command.Start(); err != nil {
		return nil, nil, err
	}
	pid := command.Process.Pid
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &status, 0, nil); err != nil {
		return nil, nil, err
	}
	if err := syscall.PtraceSetOptions(pid, syscall.PTRACE_O_TRACESYSGOOD|syscall.PTRACE_O_TRACECLONE|syscall.PTRACE_O_TRACEFORK|syscall.PTRACE_O_TRACEVFORK); err != nil {
		_ = command.Process.Kill()
		return nil, []string{"ptrace options unavailable"}, err
	}
	if err := syscall.PtraceSyscall(pid, 0); err != nil {
		return nil, nil, err
	}

	paths := []string{}
	inSyscall := make(map[int]bool)
	processes := map[int]bool{pid: true}
	for len(processes) > 0 {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			for child := range processes {
				_, _ = syscall.Wait4(child, nil, 0, nil)
			}
			return paths, nil, ctx.Err()
		default:
		}
		var waitStatus syscall.WaitStatus
		waited, err := syscall.Wait4(-1, &waitStatus, syscall.WALL, nil)
		if err != nil {
			if errors.Is(err, syscall.ECHILD) {
				break
			}
			return paths, nil, err
		}
		if waitStatus.Exited() || waitStatus.Signaled() {
			delete(processes, waited)
			continue
		}
		if waitStatus.Stopped() {
			signal := waitStatus.StopSignal()
			if signal == syscall.SIGTRAP|0x80 {
				if !inSyscall[waited] {
					var registers syscall.PtraceRegs
					if err := syscall.PtraceGetRegs(waited, &registers); err == nil {
						paths = append(paths, syscallPaths(waited, registers)...)
					}
				}
				inSyscall[waited] = !inSyscall[waited]
				signal = 0
			} else if signal == syscall.SIGTRAP {
				event := waitStatus.TrapCause()
				if event == syscall.PTRACE_EVENT_CLONE || event == syscall.PTRACE_EVENT_FORK || event == syscall.PTRACE_EVENT_VFORK {
					message, err := syscall.PtraceGetEventMsg(waited)
					if err == nil {
						processes[int(message)] = true
					}
				}
				signal = 0
			}
			if err := syscall.PtraceSyscall(waited, int(signal)); err != nil && !errors.Is(err, syscall.ESRCH) {
				return paths, nil, err
			}
		}
	}
	state, err := command.Process.Wait()
	if err != nil {
		if _, ok := err.(*os.PathError); ok {
			return paths, nil, nil
		}
		return paths, nil, err
	}
	if !state.Success() {
		return paths, nil, &exec.ExitError{ProcessState: state}
	}
	return paths, nil, nil
}

func syscallPaths(pid int, registers syscall.PtraceRegs) []string {
	switch registers.Orig_rax {
	case syscall.SYS_OPEN:
		return readPointerPaths(pid, uintptr(registers.Rdi))
	case syscall.SYS_OPENAT, syscall.SYS_NEWFSTATAT, syscall.SYS_READLINKAT:
		return readPointerPaths(pid, uintptr(registers.Rsi))
	case syscall.SYS_EXECVE, syscall.SYS_STAT, syscall.SYS_LSTAT, syscall.SYS_ACCESS, syscall.SYS_READLINK:
		return readPointerPaths(pid, uintptr(registers.Rdi))
	case syscallExecveAt:
		return readPointerPaths(pid, uintptr(registers.Rsi))
	default:
		return nil
	}
}

func readPointerPaths(pid int, address uintptr) []string {
	if address == 0 {
		return nil
	}
	content := make([]byte, 4096)
	read := 0
	for read < len(content) {
		word := make([]byte, unsafe.Sizeof(uintptr(0)))
		count, err := syscall.PtracePeekData(pid, address+uintptr(read), word)
		if err != nil || count <= 0 {
			break
		}
		for index := 0; index < count; index++ {
			if word[index] == 0 {
				if read+index == 0 {
					return nil
				}
				return []string{string(content[:read+index])}
			}
			content[read+index] = word[index]
		}
		read += count
	}
	if read > 0 {
		return []string{string(content[:read])}
	}
	return nil
}

var _ = fmt.Sprintf
