package cli

import (
	"github.com/spf13/cobra"
)

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Manage hosts",
}

func init() {
	hostCmd.AddCommand(hostNewCmd)
	hostCmd.AddCommand(hostSetupCmd)
}
