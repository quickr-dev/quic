package cli

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/quickr-dev/quic/internal/config"
	pb "github.com/quickr-dev/quic/proto"
)

const DefaultTimeout = 60 * time.Second


func executeWithClient(fn func(pb.QuicServiceClient, context.Context) error) error {
	cfg, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	return executeWithClientOnHost(cfg.SelectedHost, cfg.AuthToken, DefaultTimeout, fn)
}

func executeWithClientOnHost(host, authToken string, timeout time.Duration, fn func(pb.QuicServiceClient, context.Context) error) error {
	projectConfig, err := config.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}

	hostConfig := projectConfig.GetHostByIP(host)
	if hostConfig == nil {
		return fmt.Errorf("host %s not found in configuration", host)
	}

	if hostConfig.CertificateFingerprint == "" {
		return fmt.Errorf("no certificate fingerprint configured for host %s. Please run 'quic host setup' first", host)
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return verifyCertificateFingerprint(hostConfig.CertificateFingerprint, cs.PeerCertificates[0])
		},
	}

	conn, err := grpc.Dial(
		host+":8443",
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	if err != nil {
		return fmt.Errorf("connecting to server %s: %w", host, err)
	}
	defer conn.Close()

	md := metadata.New(map[string]string{
		"authorization": "Bearer " + authToken,
	})
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := pb.NewQuicServiceClient(conn)
	return fn(client, ctx)
}

// verifyCertificateFingerprint compares certificate fingerprints.
//
// expectedFingerprint: SHA-256 fingerprint from OpenSSL
// Example: "AA:BB:CC:DD:EE:FF:11:22:33:44:55:66:77:88:99:00:11:22:33:44:55:66:77:88:99:00:11:22:33:44:55:66"
//
// cert: X.509 certificate from TLS connection
func verifyCertificateFingerprint(expectedFingerprint string, cert *x509.Certificate) error {
	// Calculate SHA-256 fingerprint of the certificate's raw bytes
	hash := sha256.Sum256(cert.Raw)
	actualFingerprint := fmt.Sprintf("%X", hash[:])

	// Normalize expected fingerprint: remove colons, convert to uppercase
	// OpenSSL outputs: "AA:BB:CC:DD" -> we want: "AABBCCDD"
	expectedNormalized := strings.ToUpper(strings.ReplaceAll(expectedFingerprint, ":", ""))

	if expectedNormalized != actualFingerprint {
		return fmt.Errorf("certificate fingerprint mismatch: expected %s, got %s", expectedFingerprint, actualFingerprint)
	}

	return nil
}
