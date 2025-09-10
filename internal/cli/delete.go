package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	pb "github.com/quickr-dev/quic/proto"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <branch-name>",
	Short: "Delete a database branch",
	Long:  "Deletes a database branch and cleans up all associated resources",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeDelete(args[0], cmd)
	},
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
			return fmt.Errorf("deleting checkout: %w", err)
		}

		return nil
	})
}

func init() {
	deleteCmd.Flags().String("template", "", "Name of the template template containing the checkout to delete")
}
