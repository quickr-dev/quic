package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/quickr-dev/quic/internal/agent"
	"github.com/quickr-dev/quic/internal/auth"
	"github.com/quickr-dev/quic/internal/server"
	pb "github.com/quickr-dev/quic/proto"
)

func main() {
	// Load TLS credentials
	creds, err := credentials.NewServerTLSFromFile(
		"/etc/quic/certs/server.crt",
		"/etc/quic/certs/server.key",
	)
	if err != nil {
		log.Fatalf("Failed to load TLS credentials: %v", err)
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
		log.Fatalf("Failed to listen on port 8443: %v", err)
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

	// Gracefully stop the server
	grpcServer.GracefulStop()
	log.Println("Quicd server stopped")
}
