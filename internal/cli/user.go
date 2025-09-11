package cli

import (
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage users",
}

func init() {
	userCmd.AddCommand(userCreateCmd)
}
