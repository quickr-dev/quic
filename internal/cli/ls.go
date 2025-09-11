package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List branches",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeList(cmd)
	},
}

func executeList(cmd *cobra.Command) error {
	cfg, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	templateName, _ := cmd.Flags().GetString("template")
	if templateName == "" {
		templateName = cfg.DefaultTemplate
	}

	return executeWithClient(func(client pb.QuicServiceClient, ctx context.Context) error {
		req := &pb.ListCheckoutsRequest{
			RestoreName: templateName,
		}

		resp, err := client.ListCheckouts(ctx, req)
		if err != nil {
			return fmt.Errorf("listing checkouts: %w", err)
		}

		if len(resp.Checkouts) == 0 {
			fmt.Println("No checkouts found.")
			return nil
		}

		// Print header
		fmt.Printf("%-20s %-15s %-20s\n", "CLONE NAME", "CREATED BY", "CREATED AT")
		fmt.Printf("%-20s %-15s %-20s\n", "----------", "----------", "----------")

		// Print each checkout
		for _, checkout := range resp.Checkouts {
			fmt.Printf("%-20s %-15s %-20s\n",
				checkout.CloneName,
				checkout.CreatedBy,
				checkout.CreatedAt,
			)
		}

		return nil
	})
}

func init() {
	lsCmd.Flags().String("template", "", "Name of the template template to list checkouts from (optional - lists all if not specified)")
}
