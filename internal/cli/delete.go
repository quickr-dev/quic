package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	pb "github.com/quickr-dev/quic/proto"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <branch-name>",
	Short: "Delete a branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeDelete(args[0], cmd)
	},
}

func init() {
	deleteCmd.Flags().String("template", "", "Template to delete the branch from")
}

func executeDelete(branchName string, cmd *cobra.Command) error {
	templateFlag, _ := cmd.Flags().GetString("template")
	template, err := GetTemplate(templateFlag)
	if err != nil {
		return err
	}

	return executeWithClient(func(client pb.QuicServiceClient, ctx context.Context) error {
		req := &pb.DeleteCheckoutRequest{
			CloneName:   branchName,
			RestoreName: template.Name,
		}

		_, err := client.DeleteCheckout(ctx, req)
		if err != nil {
			return fmt.Errorf("deleting branch: %w", err)
		}

		return nil
	})
}
