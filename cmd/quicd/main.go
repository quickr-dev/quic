package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/quickr-dev/quic/internal/agent"
	"github.com/quickr-dev/quic/internal/auth"
	"github.com/quickr-dev/quic/internal/db"
	"github.com/quickr-dev/quic/internal/server"
	pb "github.com/quickr-dev/quic/proto"
)

func main() {
	if err := runDaemon(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runDaemon() error {
	// Initialize database
	database, err := db.InitDB()
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer database.Close()

	log.Println("✓ Init Database")

	// Load TLS credentials
	creds, err := credentials.NewServerTLSFromFile(
		"/etc/quic/certs/server.crt",
		"/etc/quic/certs/server.key",
	)
	if err != nil {
		return fmt.Errorf("failed to load TLS credentials: %w", err)
	}

	// Create agent service
	agentService := agent.NewCheckoutService()

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
