package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all database checkouts",
	Long:  "Lists all existing database checkouts with their clone names, creators, and creation timestamps",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		client, _, cleanup, err := getQuicClient()
		if err != nil {
			return err
		}
		defer cleanup()

		authCtx := getAuthContext(cfg)
		ctx, cancel := context.WithTimeout(authCtx, 10*time.Second)
		defer cancel()

		req := &pb.ListCheckoutsRequest{}

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
	},
}
