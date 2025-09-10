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

func executeCheckout(branchName string, cmd *cobra.Command) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	templateName, _ := cmd.Flags().GetString("template")
	templateName, err = cfg.GetTemplateName(templateName)
	// TODO: validate templateName against hosts in ProjectConfig @internal/config/project_config.go
	if err != nil {
		return err
	}

	return executeWithClient(func(client pb.QuicServiceClient, ctx context.Context) error {
		req := &pb.CreateCheckoutRequest{
			CloneName:   branchName,
			RestoreName: templateName,
		}

		resp, err := client.CreateCheckout(ctx, req)
		if err != nil {
			return fmt.Errorf("creating checkout: %w", err)
		}

		// TODO: get `Template.Database` from ProjectConfig based on templateName
		connectionString := formatConnectionString(resp.ConnectionString, cfg.SelectedHost)
		fmt.Println(connectionString)
		return nil
	})
}

func formatConnectionString(original, hostname string) string {
	// Replace hostname
	result := strings.Replace(original, "@localhost:", fmt.Sprintf("@%s:", hostname), 1)

	// Replace database
	result = strings.Replace(result, "/postgres", "/dexoryview_production", 1)

	return result
}

func init() {
	checkoutCmd.Flags().String("template", "", "Name of the template template to use for checkout")
}
