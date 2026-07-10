package main

import (
	"botson/internal/update"

	"github.com/spf13/cobra"
)

// newUpdateCmd is the framework for a future `botson update` -- checking
// for and applying a newer release in place. Not implemented yet; see
// internal/update.
func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "update",
		Short:             "Update Botson to the latest release (not yet implemented)",
		PersistentPreRunE: noBootstrap,
		RunE: func(cmd *cobra.Command, args []string) error {
			return update.Run(cmd.Context())
		},
	}
}
