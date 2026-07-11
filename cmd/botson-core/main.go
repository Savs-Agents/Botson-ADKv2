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
		Short: "Botson: an AI agent service with a built-in chat client",
		Long: "Botson's core is the one process that ever holds the Gemini model,\n" +
			"agent registry, and session/artifact state, exposed over a single\n" +
			"HTTP API (internal/networking/api). Run `botson core start` once --\n" +
			"every consumer from then on, including this binary's own `botson chat`,\n" +
			"talks to it purely over that API, never in-process.\n\n" +
			"Running `botson` with no subcommand opens the interactive chat client\n" +
			"(same as `botson chat`) against an already-running core.",
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

	rootCmd.AddCommand(newChatCmd(), newCoreCmd())

	// A completely bare `botson` (no subcommand, no flags at all) implies
	// `botson chat` -- rewriting argv before Cobra resolves it lets chat's
	// own PersistentPreRunE (noBootstrap) apply exactly as if the user had
	// typed it themselves, rather than this rootCmd having to juggle two
	// different PersistentPreRunE needs itself (full setupApp for `botson
	// core`, which other subcommands still rely on inheriting; no
	// bootstrap for chat).
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "chat")
	}

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
