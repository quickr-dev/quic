package cli

import (
	"github.com/spf13/cobra"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage quic templates",
}

func init() {
	templateCmd.AddCommand(templateNewCmd)
	templateCmd.AddCommand(templateSetupCmd)
}
