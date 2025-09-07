package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/quickr-dev/quic/internal/config"
	"github.com/spf13/cobra"
)

var templateNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Add a new template to quic configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateNew,
}

func init() {
	templateNewCmd.Flags().String("pg-version", "16", "PostgreSQL version")
	templateNewCmd.Flags().String("provider", "crunchybridge", "Template provider (currently only crunchybridge)")
	templateNewCmd.Flags().String("cluster-name", "", "Provider cluster name")
	templateNewCmd.Flags().String("database", "", "Database name to restore")
}

func runTemplateNew(cmd *cobra.Command, args []string) error {
	templateName := args[0]

	if templateName == "" {
		return fmt.Errorf("template name cannot be empty")
	}

	// Get values from flags first
	pgVersion, _ := cmd.Flags().GetString("pg-version")
	providerName, _ := cmd.Flags().GetString("provider")
	clusterName, _ := cmd.Flags().GetString("cluster-name")
	database, _ := cmd.Flags().GetString("database")

	// If cluster-name or database flag is not provided, use interactive prompts
	if clusterName == "" || database == "" {
		reader := bufio.NewReader(os.Stdin)

		// Prompt for PostgreSQL version if not provided via flag
		if pgVersion == "" || pgVersion == "16" {
			fmt.Print("Postgres version [16]: ")
			pgVersionInput, _ := reader.ReadString('\n')
			input := strings.TrimSpace(pgVersionInput)
			if input != "" {
				pgVersion = input
			} else if pgVersion == "" {
				pgVersion = "16"
			}
		}

		// Select data source provider (only CrunchyBridge for now)
		if providerName == "" || providerName == "crunchybridge" {
			fmt.Println("Select the source:")
			fmt.Println("  -> CrunchyBridge backup")
			providerName = "crunchybridge"
		}

		// Input CrunchyBridge cluster name
		if clusterName == "" {
			fmt.Print("Input CrunchyBridge cluster name (https://crunchybridge.com/): ")
			clusterNameInput, _ := reader.ReadString('\n')
			clusterName = strings.TrimSpace(clusterNameInput)

			if clusterName == "" {
				return fmt.Errorf("cluster name cannot be empty")
			}
		}

		// Input database name
		if database == "" {
			fmt.Print("Input database name to restore: ")
			databaseInput, _ := reader.ReadString('\n')
			database = strings.TrimSpace(databaseInput)

			if database == "" {
				return fmt.Errorf("database name cannot be empty")
			}
		}
	}

	// Load quic config
	quicConfig, err := config.LoadQuicConfig()
	if err != nil {
		return fmt.Errorf("failed to load quic config: %w", err)
	}

	// Create template
	template := config.Template{
		Name:      templateName,
		PGVersion: pgVersion,
		Database:  database,
		Provider: config.TemplateProvider{
			Name:        providerName,
			ClusterName: clusterName,
		},
	}

	// Add template to config
	if err := quicConfig.AddTemplate(template); err != nil {
		return fmt.Errorf("failed to add template: %w", err)
	}

	// Save config
	if err := quicConfig.Save(); err != nil {
		return fmt.Errorf("failed to save quic config: %w", err)
	}

	fmt.Printf("Added template '%s' (PostgreSQL %s, Database: %s, CrunchyBridge: %s) to quic.json\n", templateName, pgVersion, database, clusterName)

	return nil
}
