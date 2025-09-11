package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/quickr-dev/quic/internal/version"
	"github.com/spf13/cobra"
)

var updateCheckCommands = []string{
	"checkout",
	"delete",
	"host new",
	"host setup",
	"ls",
	"template new",
	"template setup",
	"user create",
}

var rootCmd = &cobra.Command{
	Use:   "quic",
	Short: "Database branch",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmdPath := getCommandPath(cmd)
		if slices.Contains(updateCheckCommands, cmdPath) {
			version.CheckForUpdates()
		}
	},
}

func getCommandPath(cmd *cobra.Command) string {
	var parts []string
	for c := cmd; c != nil && c.Name() != "quic"; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	if len(parts) == 0 {
		return cmd.Name()
	}
	return strings.Join(parts, " ")
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
