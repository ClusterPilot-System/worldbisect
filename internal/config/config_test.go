package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsRelativeCommand(t *testing.T) {
	cfg := Default()
	cfg.AllowedCommands = []string{"python3"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected relative command rejection")
	}
}

func TestValidateRejectsCommandSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "tool")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.AllowedCommands = []string{link}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestValidateCanonicalizesDirectory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "workspace")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.AllowedWorkingDirectories = []string{directory}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.AllowedWorkingDirectories[0] != directory {
		t.Fatalf("directory = %s", cfg.AllowedWorkingDirectories[0])
	}
}
