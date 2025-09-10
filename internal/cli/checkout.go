package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

var checkoutCmd = &cobra.Command{
	Use:   "checkout <branch-name>",
	Short: "Create a new database branch",
	Long:  "Creates a new database branch using ZFS snapshots and returns the connection string",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branchName := args[0]

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Get restore name from flag or config
		restoreName, _ := cmd.Flags().GetString("restore")
		if restoreName == "" {
			restoreName = cfg.DefaultTemplate
		}

		if restoreName == "" {
			return fmt.Errorf("restore template not specified. Use --restore flag or set selectedRestore in config")
		}

		client, serverHostname, cleanup, err := getQuicClient()
		if err != nil {
			return err
		}
		defer cleanup()

		authCtx := getAuthContext(cfg)
		ctx, cancel := context.WithTimeout(authCtx, 60*time.Second)
		defer cancel()

		req := &pb.CreateCheckoutRequest{
			CloneName:   branchName,
			RestoreName: restoreName,
		}

		resp, err := client.CreateCheckout(ctx, req)
		if err != nil {
			return fmt.Errorf("creating checkout: %w", err)
		}

		// Replace localhost with actual server hostname
		connectionString := strings.Replace(resp.ConnectionString, "@localhost:", fmt.Sprintf("@%s:", serverHostname), 1)
		// Replace database
		// TODO: configurable database selection
		connectionString = strings.Replace(connectionString, "/postgres", "/dexoryview_production", 1)

		fmt.Println(connectionString)
		return nil
	},
}

func init() {
	checkoutCmd.Flags().String("restore", "", "Name of the restore template to use for checkout")
}
