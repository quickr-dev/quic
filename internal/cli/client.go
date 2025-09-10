package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

func getQuicClient() (pb.QuicServiceClient, string, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", nil, fmt.Errorf("loading config: %w", err)
	}

	if cfg.SelectedHost == "" {
		return nil, "", nil, fmt.Errorf("no server selected in config")
	}

	if cfg.AuthToken == "" {
		return nil, "", nil, fmt.Errorf("no auth token configured. Please set your auth token in the config file")
	}

	// Accept self-signed certs in dev/test environment
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Only for dev/test!
	}

	// Create gRPC connection with TLS
	conn, err := grpc.Dial(
		cfg.SelectedHost+":8443",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, "", nil, fmt.Errorf("connecting to server %s: %w", cfg.SelectedHost, err)
	}

	client := pb.NewQuicServiceClient(conn)

	cleanup := func() {
		conn.Close()
	}

	return client, cfg.SelectedHost, cleanup, nil
}

func getAuthContext(cfg *config.Config) context.Context {
	md := metadata.New(map[string]string{
		"authorization": "Bearer " + cfg.AuthToken,
	})
	return metadata.NewOutgoingContext(context.Background(), md)
}
