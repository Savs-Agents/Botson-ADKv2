package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// boot holds the shared config/agent/session/artifact wiring, populated once
// by rootCmd's PersistentPreRunE before any subcommand's RunE executes.
var boot *appBoot

// noBootstrap skips the root command's expensive config/Gemini/agent/session
// bootstrap for subcommands that only manage a background process's
// lifecycle and never touch the agent runtime themselves.
func noBootstrap(cmd *cobra.Command, args []string) error { return nil }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "botson",
		Short: "Botson: a NATS-only AI agent service",
		Long: "Botson's core is the one process that ever holds the Gemini model,\n" +
			"agent registry, and session/artifact state. Once configured, run\n" +
			"`botson core start` -- every consumer from then on (a Discord bot,\n" +
			"a web UI, anything) talks to it purely over NATS, never through this CLI.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			b, err := setupApp(cmd.Context())
			if err != nil {
				return err
			}
			boot = b
			return nil
		},
	}

	rootCmd.AddCommand(newCoreCmd(), newUpdateCmd())

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
