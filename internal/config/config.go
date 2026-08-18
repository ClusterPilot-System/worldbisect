package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	SchemaVersion             int      `json:"schema_version"`
	Listen                    string   `json:"listen"`
	DataDir                   string   `json:"data_dir"`
	LogLevel                  string   `json:"log_level"`
	RemoteExecutionEnabled    bool     `json:"remote_execution_enabled"`
	AllowedCommands           []string `json:"allowed_commands"`
	AllowedWorkingDirectories []string `json:"allowed_working_directories"`
	Workers                   int      `json:"workers"`
	LeaseSeconds              int      `json:"lease_seconds"`
	MaxAttempts               int      `json:"max_attempts"`
	Quotas                    Quotas   `json:"quotas"`
	Tokens                    []Token  `json:"tokens"`
}

type Quotas struct {
	MaxSessions       int   `json:"max_sessions"`
	MaxWorkspaceFiles int   `json:"max_workspace_files"`
	MaxWorkspaceBytes int64 `json:"max_workspace_bytes"`
	MaxOutputBytes    int64 `json:"max_output_bytes"`
	MaxFactors        int   `json:"max_factors"`
	MaxExperiments    int   `json:"max_experiments"`
	MaxRequestBytes   int64 `json:"max_request_bytes"`
}

type Token struct {
	Name   string   `json:"name"`
	Hash   string   `json:"hash"`
	Scopes []string `json:"scopes"`
}

func Default() Config {
	return Config{
		SchemaVersion: 1,
		Listen:        "127.0.0.1:8787",
		DataDir:       "/var/lib/worldbisect",
		LogLevel:      "info",
		Workers:       2,
		LeaseSeconds:  30,
		MaxAttempts:   3,
		Quotas: Quotas{
			MaxSessions:       1000,
			MaxWorkspaceFiles: 10000,
			MaxWorkspaceBytes: 1 << 30,
			MaxOutputBytes:    8 << 20,
			MaxFactors:        512,
			MaxExperiments:    256,
			MaxRequestBytes:   1 << 20,
		},
	}
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg *Config) Validate() error {
	if cfg.SchemaVersion != 1 {
		return fmt.Errorf("unsupported config schema %d", cfg.SchemaVersion)
	}
	if cfg.Listen == "" || cfg.DataDir == "" {
		return errors.New("listen and data_dir are required")
	}
	if cfg.Workers < 1 || cfg.Workers > 64 {
		return errors.New("workers must be between 1 and 64")
	}
	if cfg.LeaseSeconds < 5 || cfg.LeaseSeconds > 3600 {
		return errors.New("lease_seconds must be between 5 and 3600")
	}
	if cfg.MaxAttempts < 1 || cfg.MaxAttempts > 20 {
		return errors.New("max_attempts must be between 1 and 20")
	}
	if cfg.Quotas.MaxSessions < 1 || cfg.Quotas.MaxWorkspaceFiles < 1 || cfg.Quotas.MaxWorkspaceBytes < 1 ||
		cfg.Quotas.MaxOutputBytes < 1024 || cfg.Quotas.MaxFactors < 1 || cfg.Quotas.MaxExperiments < 1 || cfg.Quotas.MaxRequestBytes < 1024 {
		return errors.New("all quotas must be positive and bounded")
	}
	for index, command := range cfg.AllowedCommands {
		canonical, err := canonicalExecutable(command)
		if err != nil {
			return fmt.Errorf("allowed_commands[%d]: %w", index, err)
		}
		cfg.AllowedCommands[index] = canonical
	}
	for index, directory := range cfg.AllowedWorkingDirectories {
		canonical, err := canonicalDirectory(directory)
		if err != nil {
			return fmt.Errorf("allowed_working_directories[%d]: %w", index, err)
		}
		cfg.AllowedWorkingDirectories[index] = canonical
	}
	for _, token := range cfg.Tokens {
		if token.Name == "" || !strings.HasPrefix(token.Hash, "sha256:") || len(token.Scopes) == 0 {
			return errors.New("token name, sha256 hash, and scopes are required")
		}
	}
	return nil
}

func canonicalExecutable(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("command must be an absolute path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("command symlinks are not allowed")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err = os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("command must be a regular executable file")
	}
	return filepath.Clean(resolved), nil
}

func canonicalDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("working directory must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("working directory is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func (cfg Config) LeaseDuration() time.Duration {
	return time.Duration(cfg.LeaseSeconds) * time.Second
}
