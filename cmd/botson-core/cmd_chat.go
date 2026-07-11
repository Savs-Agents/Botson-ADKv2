package main

import (
	"botson/frontends/chat"
	"botson/internal/config"

	"github.com/spf13/cobra"
)

// newChatCmd is `botson chat`: an interactive terminal chat client against
// an already-running `botson core`, over the exact same HTTP API any
// external consumer uses. It's registered both as its own subcommand and
// (see main.go) as rootCmd's default action, so bare `botson` opens chat
// too. PersistentPreRunE: noBootstrap since chat must never go through
// setupApp()'s full bootstrap (Gemini model, agent registry, session DB)
// -- it's a pure API client, not a second copy of the agent runtime.
func newChatCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:               "chat",
		Short:             "Start an interactive chat session against a running core",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(cmd, agent)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Agent to chat with (defaults to root_agent in ~/.botson/config.json)")
	return cmd
}

// runChat loads config directly (not via setupApp -- see newChatCmd's doc
// comment) and hands off to frontends/chat.Run.
func runChat(cmd *cobra.Command, agent string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return chat.Run(cmd.Context(), cfg, agent)
}
