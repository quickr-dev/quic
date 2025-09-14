package cli

import (
	"fmt"
	"os"

	"github.com/quickr-dev/quic/internal/version"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update quic to the latest version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Checking for updates (current version: %s)...\n", version.Version)

		latest, err := version.GetLatestVersion()
		if err != nil {
			fmt.Printf("Failed to check for updates: %v\n", err)
			os.Exit(1)
		}

		if !version.IsNewerVersion(version.Version, latest) {
			fmt.Printf("Already on latest version %s\n", version.Version)
			return
		}

		fmt.Printf("Updating quic %s -> %s...\n", version.Version, latest)
		if err := version.SelfUpdate(); err != nil {
			fmt.Printf("Update failed: %v\n", err)
			os.Exit(1)
		}
	},
}
