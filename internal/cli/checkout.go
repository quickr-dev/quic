package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

var checkoutCmd = &cobra.Command{
	Use:   "checkout <branch-name>",
	Short: "Create a branch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeCheckout(args[0], cmd)
	},
}

func init() {
	checkoutCmd.Flags().String("template", "", "Template to branch from")
}

func executeCheckout(branchName string, cmd *cobra.Command) error {
	templateFlag, _ := cmd.Flags().GetString("template")
	template, err := GetTemplate(templateFlag)
	if err != nil {
		return err
	}

	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("loading user config: %w", err)
	}

	return executeWithClient(func(client pb.QuicServiceClient, ctx context.Context) error {
		req := &pb.CreateCheckoutRequest{
			CloneName:   branchName,
			RestoreName: template.Name,
		}

		resp, err := client.CreateCheckout(ctx, req)
		if err != nil {
			return fmt.Errorf("creating checkout: %w", err)
		}

		connectionString := formatConnectionString(resp.ConnectionString, userCfg.SelectedHost, template.Database)
		fmt.Println(connectionString)
		return nil
	})
}

func formatConnectionString(original, hostname, database string) string {
	// Replace hostname
	result := strings.Replace(original, "@localhost:", fmt.Sprintf("@%s:", hostname), 1)

	// Replace database
	result = strings.Replace(result, "/postgres", "/"+database, 1)

	return result
}
