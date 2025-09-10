package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

const DefaultTimeout = 10 * time.Second

func validateConfig(cfg *config.UserConfig) error {
	var errors []string

	if cfg.AuthToken == "" {
		errors = append(errors, "no auth token configured. Please run 'quic login --token <token>'")
	}

	if cfg.SelectedHost == "" {
		errors = append(errors, "no server selected in config")
	}

	if cfg.SelectedHost != "" && !isValidHost(cfg.SelectedHost) {
		errors = append(errors, "selected host has invalid format")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(errors, ", "))
	}

	return nil
}

func isValidHost(host string) bool {
	return net.ParseIP(host) != nil
}

func executeWithClient(fn func(pb.QuicServiceClient, context.Context) error) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := validateConfig(cfg); err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		// base-setup.yml creates self-signed certs so we skip verification
		InsecureSkipVerify: true,
	}

	conn, err := grpc.Dial(
		cfg.SelectedHost+":8443",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithTimeout(DefaultTimeout),
	)
	if err != nil {
		return fmt.Errorf("connecting to server %s: %w", cfg.SelectedHost, err)
	}
	defer conn.Close()

	md := metadata.New(map[string]string{
		"authorization": "Bearer " + cfg.AuthToken,
	})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	client := pb.NewQuicServiceClient(conn)
	return fn(client, ctx)
}
