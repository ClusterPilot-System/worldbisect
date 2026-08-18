package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/api"
	"github.com/ClusterPilot-System/worldbisect/internal/artifact"
	"github.com/ClusterPilot-System/worldbisect/internal/auth"
	"github.com/ClusterPilot-System/worldbisect/internal/capture"
	"github.com/ClusterPilot-System/worldbisect/internal/config"
	"github.com/ClusterPilot-System/worldbisect/internal/experiment"
	"github.com/ClusterPilot-System/worldbisect/internal/model"
	"github.com/ClusterPilot-System/worldbisect/internal/oracle"
	"github.com/ClusterPilot-System/worldbisect/internal/report"
	"github.com/ClusterPilot-System/worldbisect/internal/runner"
	"github.com/ClusterPilot-System/worldbisect/internal/service"
	"github.com/ClusterPilot-System/worldbisect/internal/store"
	"github.com/ClusterPilot-System/worldbisect/internal/version"
)

const usageText = `WorldBisect - Git bisect for runtime reality

Usage:
  worldbisect capture [options] -- command [args...]
  worldbisect compare [options] -- command [args...]
  worldbisect explain [options] <analysis-id>
  worldbisect export [options] <entity-id>
  worldbisect import [options] <bundle.wcap>
  worldbisect verify [options] <certificate.json>
  worldbisect audit [options]
  worldbisect doctor [options]
  worldbisect serve [options]
  worldbisect version

Run "worldbisect help <command>" for command-specific help.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usageText)
		return nil
	}
	if args[0] == "help" {
		if len(args) == 1 {
			fmt.Fprint(stdout, usageText)
			return nil
		}
		args = append([]string{args[1], "-help"}, args[2:]...)
	}

	switch args[0] {
	case "capture":
		return runCapture(args[1:], stdout)
	case "compare":
		return runCompare(args[1:], stdout)
	case "explain":
		return runExplain(args[1:], stdout)
	case "export":
		return runExport(args[1:], stdout)
	case "import":
		return runImport(args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout)
	case "audit":
		return runAudit(args[1:], stdout)
	case "doctor":
		return runDoctor(args[1:], stdout)
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, version.String())
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func defaultStore() string {
	if value := os.Getenv("WORLDBISECT_STORE"); value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "worldbisect")
	}
	return ".worldbisect"
}

func splitCommand(args []string) ([]string, []string, error) {
	for index, argument := range args {
		if argument == "--" {
			if index == len(args)-1 {
				return nil, nil, errors.New("missing command after --")
			}
			return args[:index], args[index+1:], nil
		}
	}
	return nil, nil, errors.New("command separator -- is required")
}

func openStore(path string) (*store.Store, error) {
	return store.Open(path)
}

func runCapture(args []string, stdout io.Writer) error {
	flags, command, err := splitCommand(args)
	if err != nil {
		return err
	}
	set := flag.NewFlagSet("capture", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	workspace := set.String("workspace", "", "workspace root")
	label := set.String("label", "", "capture label")
	oracleSpec := set.String("oracle", "exit=0", "failure oracle")
	timeout := set.Duration("timeout", 2*time.Minute, "command timeout")
	output := set.String("output", "", "portable bundle output")
	format := set.String("format", "text", "text or json")
	if err := set.Parse(flags); err != nil {
		return err
	}
	root, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	if *workspace == "" {
		root, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	parsedOracle, err := oracle.Parse(*oracleSpec)
	if err != nil {
		return err
	}
	dataStore, err := openStore(*storePath)
	if err != nil {
		return err
	}
	capturer := capture.New(dataStore, runner.New())
	record, err := capturer.Capture(context.Background(), capture.Request{
		Label:     *label,
		Workspace: root,
		Command: model.CommandSpec{
			Arguments:  command,
			Directory:  root,
			TimeoutMS:  timeout.Milliseconds(),
			Environment: model.EnvironmentFromList(os.Environ()),
		},
		Oracle: parsedOracle,
	})
	if record != nil && *output != "" {
		if err := artifact.ExportCapture(dataStore, record.ID, *output); err != nil {
			return err
		}
	}
	if record != nil {
		if *format == "json" {
			return writeJSON(stdout, record)
		}
		fmt.Fprint(stdout, report.Capture(record))
	}
	return err
}

func runCompare(args []string, stdout io.Writer) error {
	flags, command, err := splitCommand(args)
	if err != nil {
		return err
	}
	set := flag.NewFlagSet("compare", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	good := set.String("good", "", "good capture ID or bundle")
	bad := set.String("bad", "", "bad capture ID or bundle")
	repetitions := set.Int("repetitions", 3, "experiment repetitions")
	maxExperiments := set.Int("max-experiments", 128, "experiment budget")
	format := set.String("format", "text", "text or json")
	certificate := set.String("certificate", "", "certificate output path")
	if err := set.Parse(flags); err != nil {
		return err
	}
	if *good == "" || *bad == "" {
		return errors.New("--good and --bad are required")
	}
	dataStore, err := openStore(*storePath)
	if err != nil {
		return err
	}
	goodID, err := resolveCapture(dataStore, *good)
	if err != nil {
		return err
	}
	badID, err := resolveCapture(dataStore, *bad)
	if err != nil {
		return err
	}
	goodCapture, err := dataStore.LoadCapture(goodID)
	if err != nil {
		return err
	}
	badCapture, err := dataStore.LoadCapture(badID)
	if err != nil {
		return err
	}
	if len(command) == 0 {
		command = append([]string(nil), badCapture.Command.Arguments...)
	}
	engine := experiment.New(dataStore, runner.New())
	analysis, err := engine.Analyze(context.Background(), experiment.Request{
		Good:           goodCapture,
		Bad:            badCapture,
		Command:        command,
		Repetitions:    *repetitions,
		MaxExperiments: *maxExperiments,
	})
	if analysis != nil && *certificate != "" {
		if err := artifact.WriteCertificate(dataStore, analysis.ID, *certificate); err != nil {
			return err
		}
	}
	if analysis != nil {
		if *format == "json" {
			return writeJSON(stdout, analysis)
		}
		fmt.Fprint(stdout, report.Analysis(analysis))
	}
	return err
}

func resolveCapture(dataStore *store.Store, reference string) (string, error) {
	if strings.HasSuffix(reference, ".wcap") || strings.HasSuffix(reference, ".tar.gz") {
		return artifact.ImportBundle(dataStore, reference)
	}
	return reference, nil
}

func runExplain(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("explain", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	format := set.String("format", "text", "text or json")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return errors.New("analysis ID is required")
	}
	dataStore, err := openStore(*storePath)
	if err != nil {
		return err
	}
	analysis, err := dataStore.LoadAnalysis(set.Arg(0))
	if err != nil {
		return err
	}
	if *format == "json" {
		return writeJSON(stdout, analysis)
	}
	fmt.Fprint(stdout, report.Analysis(analysis))
	return nil
}

func runExport(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("export", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	output := set.String("output", "", "output path")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 1 || *output == "" {
		return errors.New("entity ID and --output are required")
	}
	dataStore, err := openStore(*storePath)
	if err != nil {
		return err
	}
	if err := artifact.ExportCapture(dataStore, set.Arg(0), *output); err != nil {
		return err
	}
	fmt.Fprintln(stdout, *output)
	return nil
}

func runImport(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("import", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return errors.New("bundle path is required")
	}
	dataStore, err := openStore(*storePath)
	if err != nil {
		return err
	}
	id, err := artifact.ImportBundle(dataStore, set.Arg(0))
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, id)
	return nil
}

func runVerify(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("verify", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	publicKey := set.String("public-key", "", "public key path")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return errors.New("certificate path is required")
	}
	result, err := artifact.VerifyCertificate(set.Arg(0), *publicKey)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runAudit(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("audit", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	dataStore, err := openStore(*storePath)
	if err != nil {
		return err
	}
	result, err := dataStore.VerifyAudit()
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runDoctor(args []string, stdout io.Writer) error {
	set := flag.NewFlagSet("doctor", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	storePath := set.String("store", defaultStore(), "store directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	checks := []map[string]any{
		{"name": "platform", "ok": runtime.GOOS == "linux", "value": runtime.GOOS + "/" + runtime.GOARCH},
		{"name": "procfs", "ok": exists("/proc/self/status")},
		{"name": "mountinfo", "ok": exists("/proc/self/mountinfo")},
		{"name": "store_parent", "ok": writableParent(*storePath), "value": *storePath},
		{"name": "native_trace", "ok": runtime.GOOS == "linux" && runtime.GOARCH == "amd64", "value": "linux/amd64 only in 1.0"},
	}
	return writeJSON(stdout, map[string]any{"version": version.String(), "checks": checks})
}

func runServe(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("serve", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	configPath := set.String("config", "", "configuration file")
	listen := set.String("listen", "", "listen address override")
	storePath := set.String("store", defaultStore(), "store directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	cfg := config.Default()
	if *configPath != "" {
		loaded, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		cfg = loaded
	} else {
		cfg.DataDir = *storePath
		if raw := os.Getenv("WORLDBISECT_TOKEN"); raw != "" {
			hash := auth.HashToken(raw)
			cfg.Tokens = []config.Token{{Name: "environment", Hash: hash, Scopes: []string{"read", "capture:write", "analysis:write"}}}
		}
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	dataStore, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	runtimeService := service.New(cfg, dataStore, runner.New())
	server := api.New(cfg, dataStore, runtimeService)
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go runtimeService.Run(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	fmt.Fprintln(stdout, "WorldBisect listening on", cfg.Listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writableParent(path string) bool {
	parent := filepath.Dir(path)
	for {
		info, err := os.Stat(parent)
		if err == nil && info.IsDir() {
			file, err := os.CreateTemp(parent, ".worldbisect-doctor-")
			if err != nil {
				return false
			}
			name := file.Name()
			_ = file.Close()
			_ = os.Remove(name)
			return true
		}
		next := filepath.Dir(parent)
		if next == parent {
			return false
		}
		parent = next
	}
}

func parseDurationSeconds(value string) (int64, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	seconds := int64(duration / time.Second)
	if seconds <= 0 {
		return 0, errors.New("duration must be positive")
	}
	return seconds, nil
}

func requestJSON(client *http.Client, method, url, token, idempotency string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if idempotency != "" {
		request.Header.Set("Idempotency-Key", idempotency)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(output)
}

func integerEnv(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
