package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <branch-name>",
	Short: "Delete a database branch",
	Long:  "Deletes a database branch and cleans up all associated resources",
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
			restoreName = cfg.SelectedRestore
		}

		if restoreName == "" {
			return fmt.Errorf("restore template not specified. Use --restore flag or set selectedRestore in config")
		}

		client, _, cleanup, err := getQuicClient()
		if err != nil {
			return err
		}
		defer cleanup()

		authCtx := getAuthContext(cfg)
		ctx, cancel := context.WithTimeout(authCtx, 30*time.Second)
		defer cancel()

		req := &pb.DeleteCheckoutRequest{
			CloneName:   branchName,
			RestoreName: restoreName,
		}

		_, err = client.DeleteCheckout(ctx, req)
		if err != nil {
			return fmt.Errorf("deleting checkout: %w", err)
		}

		// Silent success - no output on successful delete
		return nil
	},
}

func init() {
	deleteCmd.Flags().String("restore", "", "Name of the restore template containing the checkout to delete")
}
