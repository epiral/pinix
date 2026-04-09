// Role:    Registry subcommand group — manage Pinix Registry packages
// Depends: cobra
// Exports: newRegistryGroupCommand

package main

import "github.com/spf13/cobra"

func newRegistryGroupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage Pinix Registry packages",
	}
	cmd.AddCommand(newRegisterCommand())
	cmd.AddCommand(newLoginCommand())
	cmd.AddCommand(newLogoutCommand())
	cmd.AddCommand(newWhoAmICommand())
	cmd.AddCommand(newSearchCommand())
	cmd.AddCommand(newPublishCommand())
	cmd.AddCommand(newDistTagCommand())
	return cmd
}
