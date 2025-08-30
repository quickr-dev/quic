package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/quickr-dev/quic/internal/agent"
	"github.com/quickr-dev/quic/internal/auth"
	"github.com/quickr-dev/quic/internal/server"
	pb "github.com/quickr-dev/quic/proto"
)

var rootCmd = &cobra.Command{
	Use:   "quicd",
	Short: "Quic daemon server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon()
	},
}

var initCmd = &cobra.Command{
	Use:   "init <dirname>",
	Short: "Initialize a pgbackrest restore to be used as template for branches",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dirname := args[0]
		stanza, _ := cmd.Flags().GetString("stanza")
		database, _ := cmd.Flags().GetString("database")

		if stanza == "" {
			return fmt.Errorf("--stanza flag is required")
		}

		if database == "" {
			return fmt.Errorf("--database flag is required")
		}

		if err := performInit(dirname, stanza, database); err != nil {
			return fmt.Errorf("init failed: %w", err)
		}

		fmt.Printf("Initialized restore template")
		return nil
	},
}

func init() {
	initCmd.Flags().String("stanza", "", "pgBackRest stanza name")
	initCmd.Flags().String("database", "", "Database name to configure for connections")
	initCmd.MarkFlagRequired("stanza")
	initCmd.MarkFlagRequired("database")

	rootCmd.AddCommand(initCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func performInit(dirname, stanza, database string) error {
	datasetPath := fmt.Sprintf("tank/%s", dirname)
	mountPath := fmt.Sprintf("/opt/quic/restores/%s", dirname)

	cmd := exec.Command("sudo", "zfs", "create", "-o", fmt.Sprintf("mountpoint=%s", mountPath), datasetPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating ZFS dataset: %w", err)
	}

	if err := exec.Command("sudo", "pgbackrest", "--stanza="+stanza, "--config=/etc/pgbackrest.conf", "restore", "--pg1-path="+mountPath).Run(); err != nil {
		return fmt.Errorf("pgbackrest restore: %w", err)
	}

	if err := exec.Command("sudo", "chown", "-R", "postgres:postgres", mountPath).Run(); err != nil {
		return fmt.Errorf("setting ownership: %w", err)
	}

	// Store metadata about the restore for future use
	metadataPath := fmt.Sprintf("%s/.quic-init-meta.json", mountPath)
	metadata := fmt.Sprintf(`{
  "dirname": "%s",
  "stanza": "%s",
  "database": "%s",
  "created_at": "%s"
}`, dirname, stanza, database, time.Now().Format(time.RFC3339))

	cmd = exec.Command("sudo", "tee", metadataPath)
	cmd.Stdin = strings.NewReader(metadata)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func runDaemon() error {
	// Load TLS credentials
	creds, err := credentials.NewServerTLSFromFile(
		"/etc/quic/certs/server.crt",
		"/etc/quic/certs/server.key",
	)
	if err != nil {
		return fmt.Errorf("failed to load TLS credentials: %w", err)
	}

	// Create agent service configuration
	config := &agent.CheckoutConfig{
		ZFSParentDataset: "tank",
		PostgresBinPath:  "/usr/lib/postgresql/16/bin",
		StartPort:        15432,
		EndPort:          16432,
	}

	// Create agent service
	agentService := agent.NewCheckoutService(config)

	// Create gRPC server with TLS and auth interceptor
	grpcServer := grpc.NewServer(
		grpc.Creds(creds),
		grpc.UnaryInterceptor(auth.UnaryAuthInterceptor()),
	)

	// Register our service
	quicServer := server.NewQuicServer(agentService)
	pb.RegisterQuicServiceServer(grpcServer, quicServer)

	// Listen on port 8443
	lis, err := net.Listen("tcp", ":8443")
	if err != nil {
		return fmt.Errorf("failed to listen on port 8443: %w", err)
	}

	log.Println("Quic gRPC server listening on :8443 with TLS")

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal, gracefully stopping server...")

	// First, shutdown checkout service (wait for active checkouts)
	log.Println("Waiting for active checkouts to complete...")
	if err := agentService.Shutdown(5 * time.Minute); err != nil {
		log.Printf("Checkout service shutdown failed: %v", err)
	} else {
		log.Println("All active checkouts completed")
	}

	// Then gracefully stop the gRPC server
	grpcServer.GracefulStop()
	log.Println("Quicd server stopped")
	return nil
}
