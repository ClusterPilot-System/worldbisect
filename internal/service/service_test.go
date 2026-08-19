package service

import (
	"context"
	"os"
	"path/filepath"
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
