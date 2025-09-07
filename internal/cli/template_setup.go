package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/quickr-dev/quic/internal/config"
	"github.com/quickr-dev/quic/internal/providers"
	pb "github.com/quickr-dev/quic/proto"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var templateSetupCmd = &cobra.Command{
	Use:   "setup [template-name]",
	Short: "Setup templates by restoring from configured sources",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTemplateSetup,
}


func runTemplateSetup(cmd *cobra.Command, args []string) error {
	// Load quic config
	quicConfig, err := config.LoadQuicConfig()
	if err != nil {
		return fmt.Errorf("failed to load quic config: %w", err)
	}

	if len(quicConfig.Hosts) == 0 {
		return fmt.Errorf("no hosts configured. Run 'quic host new' first")
	}

	// Determine which templates to setup
	var templatesToSetup []config.Template
	if len(args) == 1 {
		templateName := args[0]
		found := false
		for _, template := range quicConfig.Templates {
			if template.Name == templateName {
				templatesToSetup = []config.Template{template}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("template '%s' not found in quic.json", templateName)
		}
	} else {
		if len(quicConfig.Templates) == 0 {
			return fmt.Errorf("no templates configured. Run 'quic template new' first")
		}
		templatesToSetup = quicConfig.Templates
	}

	// Get CrunchyBridge API key from environment
	apiKey := os.Getenv("CB_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("CrunchyBridge API key not found. Please provide it:\n$ CB_API_KEY=<YOUR_KEY> quic template setup")
	}

	// Create CrunchyBridge client
	client := providers.NewCrunchyBridgeClient(apiKey)

	// Setup each template
	for _, template := range templatesToSetup {
		if err := setupTemplate(template, client, quicConfig.Hosts); err != nil {
			return fmt.Errorf("failed to setup template '%s': %w", template.Name, err)
		}
	}

	fmt.Printf("✓ Successfully setup %d template(s)\n", len(templatesToSetup))
	return nil
}


func setupTemplate(template config.Template, client *providers.CrunchyBridgeClient, hosts []config.QuicHost) error {
	fmt.Printf("\n🔄 Setting up template '%s'...\n", template.Name)

	// Validate template provider
	if template.Provider.Name != "crunchybridge" {
		return fmt.Errorf("unsupported provider: %s", template.Provider.Name)
	}

	// Find cluster
	fmt.Printf("🔍 Finding CrunchyBridge cluster '%s'...\n", template.Provider.ClusterName)
	cluster, err := client.FindClusterByName(template.Provider.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to find cluster '%s': %w", template.Provider.ClusterName, err)
	}

	if cluster.State != "ready" {
		return fmt.Errorf("cluster '%s' is not ready (state: %s)", cluster.Name, cluster.State)
	}

	fmt.Printf("✓ Found cluster: %s (ID: %s)\n", cluster.Name, cluster.ID)

	// Create backup token
	fmt.Printf("🔑 Creating backup token...\n")
	backupToken, err := client.CreateBackupToken(cluster.ID)
	if err != nil {
		return fmt.Errorf("failed to create backup token: %w", err)
	}

	fmt.Printf("✓ Created backup token (type: %s)\n", backupToken.Type)

	// Generate pgbackrest config
	pgDataPath := fmt.Sprintf("/opt/quic/%s/_restore", template.Name)
	pgbackrestConfig := backupToken.GeneratePgBackRestConfig(backupToken.Stanza, pgDataPath)

	// Setup template on each host
	for _, host := range hosts {
		fmt.Printf("\n📡 Setting up template '%s' on host %s (%s)...\n", template.Name, host.Alias, host.IP)
		
		if err := setupTemplateOnHost(template, backupToken, pgbackrestConfig, host); err != nil {
			return fmt.Errorf("failed to setup template on host %s: %w", host.Alias, err)
		}
		
		fmt.Printf("✓ Template '%s' setup complete on host %s\n", template.Name, host.Alias)
	}

	return nil
}

func setupTemplateOnHost(template config.Template, backupToken *providers.BackupToken, pgbackrestConfig string, host config.QuicHost) error {
	// Connect to agent with TLS (skip verification for self-signed certs)
	config := &tls.Config{
		InsecureSkipVerify: true,
	}
	conn, err := grpc.Dial(fmt.Sprintf("%s:8443", host.IP), grpc.WithTransportCredentials(credentials.NewTLS(config)))
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer conn.Close()

	client := pb.NewQuicServiceClient(conn)

	// Convert backup token to protobuf
	pbBackupToken := convertBackupTokenToPB(backupToken)

	// Create restore request
	req := &pb.RestoreTemplateRequest{
		TemplateName:      template.Name,
		Database:          template.Database,
		PgVersion:         template.PGVersion,
		BackupToken:       pbBackupToken,
		PgbackrestConfig:  pgbackrestConfig,
	}

	// Start restore with streaming
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	stream, err := client.RestoreTemplate(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start restore: %w", err)
	}

	// Process streaming responses
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("restore stream error: %w", err)
		}

		switch msg := resp.Message.(type) {
		case *pb.RestoreTemplateResponse_Log:
			// Print pgbackrest logs in real-time
			fmt.Printf("  %s\n", msg.Log.Line)

		case *pb.RestoreTemplateResponse_Result:
			fmt.Printf("✓ Restore completed successfully!\n")
			fmt.Printf("  Connection: %s\n", msg.Result.ConnectionString)
			fmt.Printf("  Service: %s\n", msg.Result.ServiceName)
			fmt.Printf("  Port: %d\n", msg.Result.Port)

		case *pb.RestoreTemplateResponse_Error:
			return fmt.Errorf("restore failed at step '%s': %s", msg.Error.Step, msg.Error.ErrorMessage)
		}
	}

	return nil
}

func convertBackupTokenToPB(token *providers.BackupToken) *pb.BackupToken {
	pbToken := &pb.BackupToken{
		RepoPath: token.RepoPath,
		Type:     token.Type,
		Stanza:   token.Stanza,
	}

	switch token.Type {
	case "s3":
		if token.AWS != nil {
			pbToken.CloudConfig = &pb.BackupToken_Aws{
				Aws: &pb.AWSConfig{
					S3Bucket:    token.AWS.S3Bucket,
					S3Key:       token.AWS.S3Key,
					S3KeySecret: token.AWS.S3KeySecret,
					S3Region:    token.AWS.S3Region,
					S3Token:     token.AWS.S3Token,
				},
			}
		}
	case "azure":
		if token.Azure != nil {
			pbToken.CloudConfig = &pb.BackupToken_Azure{
				Azure: &pb.AzureConfig{
					StorageAccount: token.Azure.StorageAccount,
					StorageKey:     token.Azure.StorageKey,
					Container:      token.Azure.Container,
				},
			}
		}
	case "gcs", "gcp":
		if token.GCP != nil {
			pbToken.CloudConfig = &pb.BackupToken_Gcp{
				Gcp: &pb.GCPConfig{
					Bucket:            token.GCP.Bucket,
					ServiceAccountKey: token.GCP.ServiceAccountKey,
				},
			}
		}
	}

	return pbToken
}