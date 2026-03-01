// Role:    Cobra root command for pinix CLI
// Depends: cobra
// Exports: Execute

package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "pinix",
	Short: "Pinix — decentralized runtime platform for Clips",
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
