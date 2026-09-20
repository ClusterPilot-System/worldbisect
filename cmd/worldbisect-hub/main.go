// worldbisect-hub is an experimental, summary-only team service. It is separate
// from the stable WorldBisect diagnostic daemon and cannot execute commands.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ClusterPilot-System/worldbisect/internal/hub"
	hubweb "github.com/ClusterPilot-System/worldbisect/web/hub"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "worldbisect-hub:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "Experimental WorldBisect team report hub (Linux, summaries only).\nUsage:\n  worldbisect-hub init --config hub.json\n  worldbisect-hub credential --config hub.json --subject alice --role viewer\n  worldbisect-hub audit-verify --audit-dir hub-data.audit\n  worldbisect-hub serve --config hub.json --data hub-data --listen 127.0.0.1:8090\nFor remote access use a trusted TLS reverse proxy. This service does not execute or verify analyses.")
		return nil
	}
	switch args[0] {
	case "init":
		flags := flag.NewFlagSet("init", flag.ContinueOnError)
		flags.SetOutput(errOut)
		configPath := flags.String("config", "hub.json", "private configuration file to create")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected init arguments")
		}
		tokens, err := hub.Initialize(*configPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Created %s (workspace: default). Store these tokens securely; they will not be printed again.\nRead token: %s\nWrite token: %s\n", *configPath, tokens["read"], tokens["write"])
		return nil
	case "credential":
		flags := flag.NewFlagSet("credential", flag.ContinueOnError)
		flags.SetOutput(errOut)
		configPath := flags.String("config", "hub.json", "private configuration file")
		subject := flags.String("subject", "", "operator-provisioned identity identifier")
		kind := flags.String("kind", "user", "identity kind: user or service")
		workspace := flags.String("workspace", "default", "workspace membership")
		role := flags.String("role", "viewer", "membership role: viewer, editor or publisher")
		expires := flags.Duration("expires-in", 30*24*time.Hour, "credential lifetime, maximum 8760h; 0 disables expiry")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected credential arguments")
		}
		key, token, err := hub.ProvisionCredential(*configPath, hub.CredentialOptions{SubjectID: *subject, Kind: *kind, Workspace: *workspace, Role: *role, ExpiresIn: *expires})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Created credential %s for %s in %s. Reload the running hub with SIGHUP. Store this token securely; it will not be printed again.\nToken: %s\n", key.ID, key.SubjectID, key.Workspace, token)
		return nil
	case "audit-verify":
		flags := flag.NewFlagSet("audit-verify", flag.ContinueOnError)
		flags.SetOutput(errOut)
		auditDir := flags.String("audit-dir", "hub-data.audit", "stopped server's private audit directory")
		expected := flags.String("expected-head", "", "optional externally recorded exact head SHA-256")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected audit-verify arguments")
		}
		status, err := hub.VerifyAudit(*auditDir, *expected)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(status)
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(errOut)
		configPath := flags.String("config", "hub.json", "private configuration file")
		dataDir := flags.String("data", "hub-data", "dedicated private local data directory")
		auditDir := flags.String("audit-dir", "", "private audit directory; defaults to data path plus .audit")
		listen := flags.String("listen", "127.0.0.1:8090", "listen address; remote access requires a trusted TLS reverse proxy")
		allowRemote := flags.Bool("allow-remote-listen", false, "explicitly allow a non-loopback listener behind a trusted TLS reverse proxy")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected serve arguments")
		}
		if err := validateListen(*listen, *allowRemote); err != nil {
			return err
		}
		config, err := hub.LoadConfig(*configPath)
		if err != nil {
			return err
		}
		store, err := hub.OpenStore(*dataDir, config)
		if err != nil {
			return err
		}
		defer store.Close()
		if *auditDir == "" {
			*auditDir = *dataDir + ".audit"
		}
		audit, err := hub.OpenAudit(*auditDir, config.AuditRetentionDays)
		if err != nil {
			return err
		}
		defer audit.Close()
		if err := audit.Append(hub.AuditEvent{Actor: "operator", Kind: "operator", Action: "server.start", Outcome: "accepted"}); err != nil {
			return err
		}
		handler := hub.NewServer(store, hubweb.Handler(), audit)
		reloads := make(chan os.Signal, 1)
		signal.Notify(reloads, syscall.SIGHUP)
		defer signal.Stop(reloads)
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		logger := log.New(errOut, "worldbisect-hub: ", log.LstdFlags)
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024, ErrorLog: logger}
		serveErr := make(chan error, 1)
		go func() { serveErr <- server.Serve(listener) }()
		fmt.Fprintf(out, "Experimental report hub listening on %s (submitted summaries, not independently verified proof).\n", listener.Addr())
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := server.Shutdown(shutdownCtx)
				cancel()
				if err != nil {
					_ = server.Close()
					return err
				}
				return nil
			case err := <-serveErr:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			case <-reloads:
				next, err := hub.LoadConfig(*configPath)
				if err != nil {
					_ = audit.Append(hub.AuditEvent{Actor: "operator", Kind: "operator", Action: "config.reload", Outcome: "rejected"})
					logger.Printf("configuration reload rejected: %v", err)
					continue
				}
				if err := handler.Reload(next); err != nil {
					logger.Printf("configuration reload failed: %v", err)
					continue
				}
				logger.Printf("configuration reloaded: %s", next.AccessSummary())
			case <-ticker.C:
				if err := audit.PurgeExpired(); err != nil {
					logger.Printf("audit retention sweep failed: %v", err)
				}
				if err := store.PurgeExpired(); err != nil {
					logger.Printf("retention sweep failed: %v", err)
				}
			}
		}
	default:
		return errors.New("expected init, credential, audit-verify or serve; use --help for usage")
	}
}

func validateListen(address string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if !allowRemote && (ip == nil || !ip.IsLoopback()) {
		return errors.New("use a loopback IP or explicitly set --allow-remote-listen behind a trusted TLS reverse proxy")
	}
	return nil
}
