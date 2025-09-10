package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/quickr-dev/quic/internal/config"
	"github.com/quickr-dev/quic/internal/db"
	"github.com/quickr-dev/quic/internal/ssh"
	"github.com/spf13/cobra"
)

var userCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "[ssh] Create a new user",
	Args:  cobra.ExactArgs(1),
	RunE:  runUserCreate,
}

func runUserCreate(cmd *cobra.Command, args []string) error {
	name := args[0]

	if name == "" {
		return fmt.Errorf("user name cannot be empty")
	}

	// Load quic config to get hosts
	quicConfig, err := config.LoadProjectConfig()
	if err != nil {
		return fmt.Errorf("failed to load quic config: %w", err)
	}

	if len(quicConfig.Hosts) == 0 {
		return fmt.Errorf("no hosts configured. Run 'quic host new' first")
	}

	// Generate a random token
	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("failed to generate token: %w", err)
	}

	// Create user on all configured hosts (idempotent)
	var failedHosts []string
	for _, host := range quicConfig.Hosts {
		if err := createUserOnHost(host, name, token); err != nil {
			failedHosts = append(failedHosts, fmt.Sprintf("%s (%s): %v", host.Alias, host.IP, err))
		}
	}

	if len(failedHosts) > 0 {
		return fmt.Errorf("failed to create user on some hosts:\n%s", strings.Join(failedHosts, "\n"))
	}

	// Display success message with login instructions
	fmt.Printf("User '%s' created successfully on %d host(s).\n\n", name, len(quicConfig.Hosts))
	fmt.Printf("To use this token, run:\n")
	fmt.Printf("$ quic login --token %s\n", token)

	return nil
}

func createUserOnHost(host config.QuicHost, name, token string) error {
	client, err := ssh.NewClient(host.IP)
	if err != nil {
		return fmt.Errorf("failed to connect to host %s: %w", host.IP, err)
	}

	escapedName := strings.ReplaceAll(name, "'", "''")
	escapedToken := strings.ReplaceAll(token, "'", "''")

	sqlQuery := fmt.Sprintf(`INSERT INTO users (name, token) VALUES ('%s', '%s') ON CONFLICT(name) DO UPDATE SET token = excluded.token, created_at = CURRENT_TIMESTAMP;`,
		escapedName, escapedToken)

	execCmd := fmt.Sprintf(`sqlite3 %s "%s"`, db.DBPath, sqlQuery)

	if _, err := client.RunCommand(execCmd); err != nil {
		return fmt.Errorf("failed to create user in database: %w", err)
	}

	return nil
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
