package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"botson/internal/automode"
	"botson/internal/daemon"
	"botson/internal/networking/api"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const coreDaemonName = "core"
const coreDisplayName = "Botson core"

// newCoreCmd starts Botson's core: the only process that ever holds the
// Gemini model, agent registry, and session/artifact services, and the
// only thing any consumer -- a Discord bot, a web UI, anything -- ever
// talks to. It's a single HTTP server (internal/networking/api) exposing
// ADK's own REST/A2A surface (reverse-proxied into an internally-run ADK
// backend) and Botson's own settings/agents/sessions/dashboard routes
// side by side, both behind one bearer-token auth middleware. There is no
// other interface in this binary; nothing about this command dispatches
// to a TUI or any other in-process consumer.
func newCoreCmd() *cobra.Command {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:   "core",
		Short: "Start Botson's shared core: the HTTP API any interface talks to",
		RunE: func(cmd *cobra.Command, args []string) error {
			h, p := resolveHostPort(cmd, host, port)
			return runCore(cmd.Context(), h, p)
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host to bind Botson's HTTP API server to")
	cmd.Flags().IntVar(&port, "port", 4222, "Port to bind Botson's HTTP API server to")

	cmd.AddCommand(newCoreStartCmd(), newCoreStopCmd(), newCoreStatusCmd())
	return cmd
}

// resolveHostPort returns the host/port to actually use: boot.Config's
// persisted defaults (~/.botson/config.json), overridden only by a flag the
// user actually passed on this invocation (not just its static --help
// default) -- so config.json remains the real default and a flag is an
// explicit, non-persisted, per-run override.
func resolveHostPort(cmd *cobra.Command, flagHost string, flagPort int) (string, int) {
	host := boot.Config.Host
	if cmd.Flags().Changed("host") {
		host = flagHost
	}
	port := boot.Config.Port
	if cmd.Flags().Changed("port") {
		port = flagPort
	}
	return host, port
}

// coreDaemonChildArgs builds the argv used to relaunch this executable as a
// detached background process. It is exactly the plain `core` subcommand a
// user would type themselves -- runCore registers daemon state regardless
// of how it was launched (see its doc comment), so there's no separate
// hidden child command to maintain. --host/--port are only forwarded when
// the user actually passed them here: otherwise the detached child resolves
// its own default from a freshly-read config.json at its own startup,
// exactly like a plain foreground `botson core` would.
func coreDaemonChildArgs(cmd *cobra.Command, host string, port int) []string {
	args := []string{"core"}
	if cmd.Flags().Changed("host") {
		args = append(args, "--host="+host)
	}
	if cmd.Flags().Changed("port") {
		args = append(args, "--port="+strconv.Itoa(port))
	}
	return args
}

func newCoreStartCmd() *cobra.Command {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:               "start",
		Short:             "Start the core as a detached background process",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to resolve current directory: %w", err)
			}
			pid, logPath, err := daemon.Start(coreDaemonName, coreDisplayName, wd, coreDaemonChildArgs(cmd, host, port))
			if err != nil {
				return err
			}
			fmt.Printf("Started %s in background (pid %d).\nLogs: %s\n", coreDisplayName, pid, logPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Host to bind Botson's HTTP API server to")
	cmd.Flags().IntVar(&port, "port", 4222, "Port to bind Botson's HTTP API server to")
	return cmd
}

func newCoreStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "stop",
		Short:             "Stop the background core",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.Stop(coreDaemonName, coreDisplayName, force); err != nil {
				return err
			}
			fmt.Printf("%s offline.\n", coreDisplayName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force-kill the background process instead of asking it to shut down gracefully")
	return cmd
}

func newCoreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Show whether the background core is running",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := daemon.GetStatus(coreDaemonName, coreDisplayName)
			if err != nil {
				return err
			}
			if !status.Running {
				fmt.Printf("%s: not running\n", coreDisplayName)
				return nil
			}
			fmt.Printf("%s: running (pid %d, started %s)\n", coreDisplayName, status.PID, status.StartedAt.Format(time.RFC3339))
			return nil
		},
	}
}

// runCore starts Botson's core and registers it in the shared
// daemon-state/control-channel system (internal/daemon) so `botson core
// status/stop` can find and manage it. This happens no matter how the
// process was launched: directly (`botson core`), detached (`core start`),
// or under an external supervisor like systemd (a plain `ExecStart=botson
// core` unit works fine here -- systemd doesn't need this process to
// self-detach).
func runCore(ctx context.Context, host string, port int) error {
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ln, ctrlPort, err := daemon.StartControlListener(cancel)
	if err != nil {
		return fmt.Errorf("failed to start control listener: %w", err)
	}
	defer ln.Close()

	if err := daemon.WriteState(coreDaemonName, daemon.State{
		PID:       os.Getpid(),
		Port:      ctrlPort,
		StartedAt: time.Now(),
		Meta:      map[string]string{"apiHost": host, "apiPort": strconv.Itoa(port)},
	}); err != nil {
		return fmt.Errorf("failed to write daemon state: %w", err)
	}
	defer daemon.RemoveState(coreDaemonName)

	return runCoreServer(daemonCtx, host, port, false)
}

// runCoreServer is the actual core -- a single HTTP API server (see
// newCoreCmd's doc comment) plus the automode background worker -- with no
// daemon-state registration of its own. quiet suppresses the startup
// banner. Blocks until ctx is done (or the server exits unexpectedly), then
// shuts everything down.
func runCoreServer(ctx context.Context, host string, port int, quiet bool) error {
	if !quiet {
		provider := boot.Config.Provider
		if provider == "" {
			provider = "gemini"
		}
		fmt.Printf("Starting Botson's core on http://%s:%d (bearer-token authenticated)... please do not close this window.\n", host, port)
		// The model is built once, here at boot, from whatever provider/
		// model/API key were in config.json at this moment -- a later
		// settings change updates config.json and GET /botson/settings's
		// reply, but NOT this already-running process's model, until it's
		// restarted. Printing the actually-active provider/model makes
		// that mismatch obvious instead of a confusing wrong-provider
		// error days later.
		fmt.Printf("Provider: %s, model: %s\n", provider, boot.Config.ModelName)
	}

	srv, err := api.New(api.Config{
		Host:      host,
		Port:      port,
		AuthToken: boot.Config.ApiAuthToken,
		ADK:       *boot.Launcher,
		// Give it real headroom above the longest normal tool call: a real
		// agentic turn (many sequential tool calls, each its own model
		// round trip) can run for minutes, not seconds -- see
		// internal/networking/api/adkbackend.go's serverWriteTimeout doc
		// comment for the matching fix on the local ADK REST server's own
		// http.Server.WriteTimeout.
		RequestTimeout: 8 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("failed to configure the API server: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.Run(gctx) })
	g.Go(func() error {
		return automode.Run(gctx, fmt.Sprintf("http://127.0.0.1:%d", port), boot.Config.ApiAuthToken, boot.Launcher)
	})
	if err := g.Wait(); err != nil {
		return fmt.Errorf("core server execution failed: %w", err)
	}
	return nil
}
