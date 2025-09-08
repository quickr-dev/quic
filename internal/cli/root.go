package cli

import (
	"fmt"
	"os"

	"github.com/quickr-dev/quic/internal/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "quic",
	Short: "Database branching",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip update check for version and update commands
		if cmd.Name() == "version" || cmd.Name() == "update" {
			return
		}

		// Auto-update on every command
		version.CheckForUpdates()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(hostCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(templateCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
}
