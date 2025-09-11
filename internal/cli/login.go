package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/quickr-dev/quic/internal/config"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Required to execute (most) commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		token, _ := cmd.Flags().GetString("token")
		if token == "" {
			return fmt.Errorf("token is required. Use --token flag")
		}

		cfg, err := config.LoadUserConfig()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		cfg.AuthToken = token

		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Println("Authentication token saved successfully")
		return nil
	},
}

func init() {
	loginCmd.Flags().String("token", "", "Authentication token")
}
