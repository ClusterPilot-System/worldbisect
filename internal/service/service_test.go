package service

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/config"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
)

func serviceFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "tool")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.RemoteExecutionEnabled = true
	cfg.AllowedCommands = []string{executable}
	cfg.AllowedWorkingDirectories = []string{allowed}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	dataStore, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, dataStore, runner.New()), executable, allowed
}

func TestRejectsBasenameBypass(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	attackerDir := filepath.Join(t.TempDir(), "attacker")
	if err := os.MkdirAll(attackerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	attacker := filepath.Join(attackerDir, filepath.Base(executable))
	if err := os.WriteFile(attacker, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{attacker}, Directory: allowed})
	if err == nil {
		t.Fatal("basename bypass accepted")
	}
}

func TestRejectsRelativeCommandAndPathLookup(t *testing.T) {
	service, _, allowed := serviceFixture(t)
	for _, command := range []string{"tool", "./tool"} {
		if _, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{command}, Directory: allowed}); err == nil {
			t.Fatalf("relative command %q accepted", command)
		}
	}
}

func TestRejectsHardlinkSubstitution(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	hardlink := filepath.Join(filepath.Dir(executable), "hardlink")
	if err := os.Link(executable, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{hardlink}, Directory: allowed}); err == nil {
		t.Fatal("hardlink path accepted")
	}
}

func TestRejectsSymlinkCommand(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	link := filepath.Join(filepath.Dir(executable), "link")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	if _, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{link}, Directory: allowed}); err == nil {
		t.Fatal("symlink command accepted")
	}
}

func TestWorkingDirectorySymlinkSwapDetected(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	link := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(allowed, link); err != nil {
		t.Fatal(err)
	}
	binding, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{executable}, Directory: link})
	if err != nil {
		t.Fatal(err)
	}
	defer binding.ExecutableFile.Close()
	defer binding.DirectoryFile.Close()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	_, err = service.runner.Run(context.Background(), runner.Request{
		Command:    []string{executable},
		Timeout:    time.Second,
		Executable: binding,
	})
	if err != nil {
		t.Fatalf("descriptor-bound directory should remain valid after symlink swap: %v", err)
	}
}

func TestExecutableInPlaceModificationDetected(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	binding, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{executable}, Directory: allowed})
	if err != nil {
		t.Fatal(err)
	}
	defer binding.ExecutableFile.Close()
	defer binding.DirectoryFile.Close()
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := service.runner.Run(context.Background(), runner.Request{Command: []string{executable}, Timeout: time.Second, Executable: binding}); err == nil {
		t.Fatal("modified executable accepted")
	}
}

func TestWorkingDirectoryRejectsTraversalAndSymlinkEscape(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	outside := allowed + "-outside"
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		outside,
		allowed + "/../" + filepath.Base(outside),
		link,
		"../" + filepath.Base(outside),
	} {
		t.Run(path, func(t *testing.T) {
			binding, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{executable}, Directory: path})
			if binding != nil {
				binding.ExecutableFile.Close()
				binding.DirectoryFile.Close()
			}
			if err == nil {
				t.Fatal("working directory outside the configured tree was authorized")
			}
		})
	}
}

func TestDirectoryOpenRejectsEscapeAfterCanonicalization(t *testing.T) {
	_, _, allowed := serviceFixture(t)
	directory := filepath.Join(allowed, "nested")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(allowed, resolved)
	if err != nil {
		t.Fatal(err)
	}
	// Replace a validated component before the open, exactly at the old
	// check/use boundary. The open itself must stay inside the configured root.
	if err := os.Rename(directory, directory+"-original"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, directory); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{relative, "..", "../" + filepath.Base(outside), outside} {
		file, err := openDirectoryBeneath(allowed, path)
		if file != nil {
			file.Close()
		}
		if err == nil {
			t.Fatalf("bounded open accepted escaping path %q", path)
		}
	}
}

func TestAllowsNestedWorkingDirectory(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	directory := filepath.Join(allowed, "nested", "workspace")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	binding, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{executable}, Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer binding.ExecutableFile.Close()
	defer binding.DirectoryFile.Close()
	if binding.DirectoryPath != directory || binding.ExecutablePath != executable {
		t.Fatal("binding did not preserve the authorized canonical paths")
	}
	if _, err := service.runner.Run(context.Background(), runner.Request{Command: []string{executable}, Timeout: time.Second, Executable: binding}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsNonDirectoryTargetsWithoutBlocking(t *testing.T) {
	for _, kind := range []string{"regular", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			service, executable, allowed := serviceFixture(t)
			path := filepath.Join(allowed, "not-a-directory")
			if kind == "fifo" {
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			expectAuthorizationRejection(t, service, executable, path)
		})
	}
}

func TestRejectsReplacedWorkingDirectoryRootWithoutBlocking(t *testing.T) {
	service, executable, allowed := serviceFixture(t)
	if err := os.Remove(allowed); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(allowed, 0o600); err != nil {
		t.Fatal(err)
	}
	expectAuthorizationRejection(t, service, executable, allowed)
}

func TestRejectsReplacedExecutableWithoutBlocking(t *testing.T) {
	for _, kind := range []string{"fifo", "directory", "non-executable", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			service, executable, allowed := serviceFixture(t)
			if err := os.Remove(executable); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "fifo":
				err = syscall.Mkfifo(executable, 0o700)
			case "directory":
				err = os.Mkdir(executable, 0o700)
			case "non-executable":
				err = os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o600)
			case "symlink":
				target := executable + "-replacement"
				if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(target, executable)
			}
			if err != nil {
				t.Fatal(err)
			}
			expectAuthorizationRejection(t, service, executable, allowed)
		})
	}
}

func expectAuthorizationRejection(t *testing.T, service *Service, executable, directory string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		binding, err := service.authorizeExecution(model.CommandSpec{Arguments: []string{executable}, Directory: directory})
		if binding != nil {
			binding.ExecutableFile.Close()
			binding.DirectoryFile.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("non-directory workspace or unsafe executable was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authorization blocked while opening a special file")
	}
}
