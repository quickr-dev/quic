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
	_, _, _, err := ensureCrunchyBridgeBackup(t, quicE2eClusterName)
	require.NoError(t, err, "ensureCrunchyBridgeBackup should succeed")
	vmIP := ensureFreshVM(t, QuicCheckoutVM)

	// Setup host
	cleanupQuicConfig(t)
	runQuic(t, "host", "new", vmIP, "--devices", VMDevices)
	hostSetupOutput := runQuicHostSetupWithAck(t, []string{QuicCheckoutVM})
	t.Log(hostSetupOutput)

	// Create user and login
	userOutput, err := runQuic(t, "user", "create", "Test User")
	require.NoError(t, err, "quic user create should succeed\nOutput: %s", userOutput)

	token := extractTokenFromCheckoutOutput(t, userOutput)
	require.NotEmpty(t, token, "Token should be extracted from user create output")

	loginOutput, err := runQuic(t, "login", "--token", token)
	require.NoError(t, err, "quic login should succeed\nOutput: %s", loginOutput)

	// Create template
	templateName := fmt.Sprintf("test-%d", time.Now().UnixNano())
	templateOutput, err := runQuic(t, "template", "new", templateName,
		"--pg-version", "16",
		"--cluster-name", quicE2eClusterName,
		"--database", "quic_test")
	require.NoError(t, err, "quic template new should succeed\nOutput: %s", templateOutput)

	// Setup template with API key from environment
	apiKey := getRequiredTestEnv("CB_API_KEY")
	require.NotEmpty(t, apiKey, "CB_API_KEY is required")

	// Set CB_API_KEY environment variable for the command
	os.Setenv("CB_API_KEY", apiKey)
	defer os.Unsetenv("CB_API_KEY")

	t.Log("Running quic template setup...")
	templateSetupOutput, err := runQuic(t, "template", "setup", templateName)
	require.NoError(t, err, "quic template setup should succeed\nOutput: %s", templateSetupOutput)
	t.Log(templateSetupOutput)
	t.Log("✓ Finished quic template setup")

	// Create branch
	branchName := fmt.Sprintf("test-branch-%d", time.Now().UnixNano())
	checkoutOutput, err := retryCheckoutUntilReady(t, branchName, templateName, 30*time.Second)
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

func extractTokenFromCheckoutOutput(t *testing.T, output string) string {
	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		if strings.Contains(line, "$ quic login --token") {
			parts := strings.Fields(line)
			require.GreaterOrEqual(t, len(parts), 4, "Token line should have at least 4 parts")
			return parts[len(parts)-1] // Last part should be the token
		}
	}
	t.Fatal("Could not find token line in output")
	return ""
}

func retryCheckoutUntilReady(t *testing.T, branchName, templateName string, timeout time.Duration) (string, error) {
	startTime := time.Now()
	deadline := startTime.Add(timeout)
	interval := 1 * time.Second
	expectedErrorMessage := "template is still in recovery mode and not ready for branching"

	t.Log("Attempting to checkout branch")

	for time.Now().Before(deadline) {
		checkoutOutput, err := runQuic(t, "checkout", branchName, "--template", templateName)

		if err == nil {
			elapsed := time.Since(startTime)
			t.Logf("✓ Branch checkout succeeded after %v", elapsed)
			return checkoutOutput, nil
		}

		// Check both error message and command output for expected error
		if strings.Contains(checkoutOutput, expectedErrorMessage) || strings.Contains(err.Error(), expectedErrorMessage) {
			elapsed := time.Since(startTime).Round(time.Second)
			t.Logf("Template not ready yet (%v elapsed)", elapsed)
		} else {
			return "", fmt.Errorf("unexpected error during checkout: %s (output: %s)", err.Error(), strings.TrimSpace(checkoutOutput))
		}

		time.Sleep(interval)
	}

	return "", fmt.Errorf("checkout failed: template not ready after %v timeout", timeout)
}
