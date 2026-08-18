package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClusterPilot-System/worldbisect/internal/config"
)

func TestInitWritesHashedTokenOnly(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	dataDir := filepath.Join(root, "data")
	var output bytes.Buffer
	if err := run([]string{"init", "--config", configPath, "--data-dir", dataDir}, &output, &output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	marker := "Initial bearer token (shown once): "
	index := strings.Index(text, marker)
	if index < 0 {
		t.Fatalf("missing token in output: %s", text)
	}
	token := strings.TrimSpace(text[index+len(marker):])
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte(token)) {
		t.Fatal("raw token persisted in config")
	}
	var cfg config.Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tokens) != 1 || cfg.Tokens[0].Hash == "" {
		t.Fatal("hashed token missing")
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
}
