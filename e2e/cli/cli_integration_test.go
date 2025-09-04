package e2e_cli

import (
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIIntegration(t *testing.T) {
	// Get VM IP address
	vmIP := getVMIP(t)

	// Test TLS connection to gRPC server on port 8443
	t.Run("ConnectToGRPCServer", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", vmIP+":8443", 3*time.Second)
		require.NoError(t, err, "Should connect to gRPC server on port 8443")
		conn.Close()
	})

	// Test CLI checkout command
	t.Run("CLICheckout", func(t *testing.T) {
		branchName := "cli-test-" + randomString(6)

		// Override server selection to use VM
		originalConfig := backupConfig(t)
		defer restoreConfig(t, originalConfig)
		createTestConfig(t, vmIP)

		// Run checkout command
		cmd := exec.Command("../../bin/quic", "checkout", branchName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("CLI error output: %s", string(output))
		}
		require.NoError(t, err, "CLI checkout should succeed")

		// Verify connection string format
		connStr := strings.TrimSpace(string(output))
		assert.Contains(t, connStr, "postgres://admin:")
		assert.Contains(t, connStr, "@localhost:")
		assert.Contains(t, connStr, "/postgres?sslmode=disable")

		// Test CLI delete command
		t.Run("CLIDelete", func(t *testing.T) {
			cmd := exec.Command("../../bin/quic", "delete", branchName)
			output, err := cmd.Output()
			require.NoError(t, err, "CLI delete should succeed")

			// Delete should have no output (silent success)
			assert.Empty(t, strings.TrimSpace(string(output)), "Delete should be silent")
		})
	})
}

