package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/api"
	"github.com/ClusterPilot-System/worldbisect/internal/auth"
	"github.com/ClusterPilot-System/worldbisect/internal/config"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/service"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
	"github.com/ClusterPilot-System/worldbisect/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stdout, "usage: worldbisectd init|run|version")
		return nil
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout)
	case "run":
		return runServer(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, version.String())
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("init", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	configPath := set.String("config", "/etc/worldbisect/config.json", "configuration file")
	dataDir := set.String("data-dir", "/var/lib/worldbisect", "data directory")
	listen := set.String("listen", "127.0.0.1:8787", "listen address")
	force := set.Bool("force", false, "replace existing configuration")
	if err := set.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*configPath); err == nil && !*force {
		return fmt.Errorf("configuration already exists: %s", *configPath)
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	cfg := config.Default()
	cfg.Listen = *listen
	cfg.DataDir = *dataDir
	cfg.Tokens = []config.Token{{
		Name:   "initial-admin",
		Hash:   auth.HashToken(token),
		Scopes: []string{"read", "capture:write", "analysis:write", "admin"},
	}}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*configPath), 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(*dataDir, 0o750); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := atomicWrite(*configPath, encoded, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Configuration: %s\nData directory: %s\nInitial bearer token (shown once): %s\n", *configPath, *dataDir, token)
	return nil
}

func runServer(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("run", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	configPath := set.String("config", "/etc/worldbisect/config.json", "configuration file")
	if err := set.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	dataStore, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtimeService := service.New(cfg, dataStore, runner.New())
	go runtimeService.Run(ctx)
	server := api.New(cfg, dataStore, runtimeService)
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	fmt.Fprintln(stdout, "WorldBisect daemon listening on", cfg.Listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".worldbisect-config-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
