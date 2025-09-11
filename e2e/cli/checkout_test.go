package e2e_cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuicCheckout(t *testing.T) {
	checkoutOutput, templateName, branchName, err := setupQuicCheckout(t)
	require.NoError(t, err, "quic checkout should succeed\nOutput: %s", checkoutOutput)

	// Verify connection string is returned
	require.Contains(t, checkoutOutput, "postgresql://admin")

	// Now validate the checkout was properly created on the VM
	t.Run("ValidateZFSClone", func(t *testing.T) {
		cloneDatasetName := fmt.Sprintf("tank/%s/%s", templateName, branchName)

		// Verify clone dataset was created
		datasetCheckOutput := runShell(t, "multipass", "exec", QuicCheckoutVM, "--", "sudo", "zfs", "list", cloneDatasetName)
		require.Contains(t, datasetCheckOutput, cloneDatasetName, "ZFS clone dataset should exist")

		// Verify clone has correct mountpoint
		expectedMountpoint := fmt.Sprintf("/opt/quic/%s/%s", templateName, branchName)
		mountpointOutput := runInVM(t, QuicCheckoutVM, "sudo zfs get -H -o value mountpoint", cloneDatasetName)
		actualMountpoint := strings.TrimSpace(mountpointOutput)
		require.Equal(t, expectedMountpoint, actualMountpoint, "Clone mountpoint should match expected")
	})

	t.Run("ValidatePostgreSQLConfiguration", func(t *testing.T) {
		clonePath := fmt.Sprintf("/opt/quic/%s/%s", templateName, branchName)

		// Verify standby and recovery files are removed in clone
		runInVM(t, QuicCheckoutVM, "sudo test ! -f", fmt.Sprintf("%s/standby.signal", clonePath))
		runInVM(t, QuicCheckoutVM, "sudo test ! -f", fmt.Sprintf("%s/recovery.signal", clonePath))
		runInVM(t, QuicCheckoutVM, "sudo test ! -f", fmt.Sprintf("%s/recovery.conf", clonePath))

		// Verify postgresql.auto.conf contains clone configuration
		autoConfPath := fmt.Sprintf("%s/postgresql.auto.conf", clonePath)
		autoConfOutput := runInVM(t, QuicCheckoutVM, "sudo cat", autoConfPath)
		require.Contains(t, autoConfOutput, "# Clone instance", "should contain clone identifier")
		require.Contains(t, autoConfOutput, "archive_mode = 'off'", "should disable archiving")
		require.Contains(t, autoConfOutput, "restore_command = ''", "should clear restore command")

		// Verify postgresql.conf has clone-optimized settings
		postgresqlConfPath := fmt.Sprintf("%s/postgresql.conf", clonePath)
		postgresqlConfOutput := runInVM(t, QuicCheckoutVM, "sudo cat", postgresqlConfPath)
		require.Contains(t, postgresqlConfOutput, "listen_addresses = '*'", "should allow external connections")
		require.Contains(t, postgresqlConfOutput, "max_connections = 5", "should have clone-optimized connection limit")
		require.Contains(t, postgresqlConfOutput, "wal_level = minimal", "should use minimal WAL level")
		require.Contains(t, postgresqlConfOutput, "synchronous_commit = off", "should disable synchronous commit")

		// Verify pg_hba.conf allows admin user access
		pgHbaConfPath := fmt.Sprintf("%s/pg_hba.conf", clonePath)
		pgHbaConfOutput := runInVM(t, QuicCheckoutVM, "sudo cat", pgHbaConfPath)
		require.Contains(t, pgHbaConfOutput, "host    all             admin           0.0.0.0/0               md5", "should allow admin user from any IP")
	})

	t.Run("ValidatePostgreSQLService", func(t *testing.T) {
		// Verify systemd service was created and is running
		serviceName := fmt.Sprintf("quic-%s-%s", templateName, branchName)
		serviceStatusOutput := runInVM(t, QuicCheckoutVM, "sudo systemctl is-active", serviceName)
		require.Contains(t, serviceStatusOutput, "active", "PostgreSQL clone service should be active")

		// Verify postmaster.pid exists and contains correct information
		clonePath := fmt.Sprintf("/opt/quic/%s/%s", templateName, branchName)
		postmasterPidPath := fmt.Sprintf("%s/postmaster.pid", clonePath)
		runInVM(t, QuicCheckoutVM, "sudo test -f", postmasterPidPath)

		postmasterContent := runInVM(t, QuicCheckoutVM, "sudo cat", postmasterPidPath)
		require.Contains(t, postmasterContent, clonePath, "postmaster.pid should contain correct data directory")
	})

	t.Run("ValidateMetadataFile", func(t *testing.T) {
		clonePath := fmt.Sprintf("/opt/quic/%s/%s", templateName, branchName)
		metadataPath := fmt.Sprintf("%s/.quic-meta.json", clonePath)

		// Verify metadata file exists
		runInVM(t, QuicCheckoutVM, "sudo test -f", metadataPath)

		// Read and verify metadata content
		metadataOutput := runInVM(t, QuicCheckoutVM, "sudo cat", metadataPath)
		require.Contains(t, metadataOutput, branchName, "metadata should contain branch name")
		require.Contains(t, metadataOutput, "port", "metadata should contain port")
		require.Contains(t, metadataOutput, "clone_path", "metadata should contain clone_path")
		require.Contains(t, metadataOutput, "admin_password", "metadata should contain admin_password")
		require.Contains(t, metadataOutput, "created_by", "metadata should contain created_by")
	})

	t.Run("ValidatePostgreSQLConnectivity", func(t *testing.T) {
		// Extract port from the checkout output connection string
		connectionString := strings.TrimSpace(checkoutOutput)

		// Parse port from connection string (format: postgresql://admin:password@ip:port/database)
		parts := strings.Split(connectionString, ":")
		require.True(t, len(parts) >= 3, "connection string should have port")
		portPart := strings.Split(parts[len(parts)-1], "/")[0]

		// Test PostgreSQL readiness
		runInVM(t, QuicCheckoutVM, "sudo -u postgres pg_isready -p", portPart)

		// Test recovery status (should not be in recovery mode)
		recoveryOutput := psqlBranch(t, templateName, branchName, "SELECT pg_is_in_recovery()")
		require.Contains(t, recoveryOutput, "f", "PostgreSQL should not be in recovery mode")

		// Test querying test data
		usersOutput := psqlBranch(t, templateName, branchName, "SELECT COUNT(*) FROM users")
		require.Contains(t, usersOutput, "5", "Should have 5 users from test setup")
	})

	t.Run("ValidateFirewallConfiguration", func(t *testing.T) {
		// Extract port from the checkout output connection string
		connectionString := strings.TrimSpace(checkoutOutput)
		parts := strings.Split(connectionString, ":")
		require.True(t, len(parts) >= 3, "connection string should have port")
		portPart := strings.Split(parts[len(parts)-1], "/")[0]

		// Verify UFW rule was added for the port
		ufwOutput := runInVM(t, QuicCheckoutVM, "sudo ufw status")
		portRule := fmt.Sprintf("%s/tcp", portPart)
		require.Contains(t, ufwOutput, portRule, "UFW should contain rule for checkout port")
	})
}

