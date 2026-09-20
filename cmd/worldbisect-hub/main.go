// worldbisect-hub is an experimental, summary-only team service. It is separate
// from the stable WorldBisect diagnostic daemon and cannot execute commands.
package main

import (
	"context"
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
		fmt.Fprintln(out, "Experimental WorldBisect team report hub (Linux, summaries only).\nUsage:\n  worldbisect-hub init --config hub.json\n  worldbisect-hub serve --config hub.json --data hub-data --listen 127.0.0.1:8090\nFor remote access use a trusted TLS reverse proxy. This service does not execute or verify analyses.")
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
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(errOut)
		configPath := flags.String("config", "hub.json", "private configuration file")
		dataDir := flags.String("data", "hub-data", "dedicated private local data directory")
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
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		logger := log.New(errOut, "worldbisect-hub: ", log.LstdFlags)
		server := &http.Server{Handler: hub.NewHandler(store, hubweb.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024, ErrorLog: logger}
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
			case <-ticker.C:
				if err := store.PurgeExpired(); err != nil {
					logger.Printf("retention sweep failed: %v", err)
				}
			}
		}
	default:
		return errors.New("expected init or serve; use --help for usage")
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
