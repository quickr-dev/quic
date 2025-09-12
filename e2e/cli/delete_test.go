package e2e_cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicDelete(t *testing.T) {
	// Setup checkout using the common setup function
	checkoutOutput, templateName, branchName, err := setupQuicCheckout(t, QuicDeleteVM)
	require.NoError(t, err, "checkout setup should succeed")
	require.Contains(t, checkoutOutput, "postgresql://admin", "checkout should return connection string")

	// Execute the quic delete CLI command
	deleteOutput, err := runQuic(t, "delete", branchName)
	require.NoError(t, err, "quic delete should succeed\nOutput: %s", deleteOutput)

	t.Run("ValidateZFSCloneDeleted", func(t *testing.T) {
		cloneDatasetName := fmt.Sprintf("tank/%s/%s", templateName, branchName)

		output := runInVM(t, QuicDeleteVM, "zfs list")
		require.NotContains(t, output, cloneDatasetName)
	})

	t.Run("ValidateZFSSnapshotDeleted", func(t *testing.T) {
		restoreDatasetName := fmt.Sprintf("tank/%s/_restore", templateName)
		snapshotName := fmt.Sprintf("%s@%s", restoreDatasetName, branchName)

		output := runInVM(t, QuicDeleteVM, "zfs list -t snapshot")
		require.NotContains(t, output, snapshotName)
	})

	t.Run("ValidateSystemdServiceRemoved", func(t *testing.T) {
		serviceName := fmt.Sprintf("quic-%s-%s", templateName, branchName)

		// Verify systemd service is not running
		cmd := fmt.Sprintf("sudo systemctl is-active %s 2>/dev/null || echo 'inactive'", serviceName)
		serviceStatusOutput := runInVM(t, QuicDeleteVM, cmd)
		require.NotContains(t, serviceStatusOutput, "active", "PostgreSQL clone service should not be active after delete")

		// Verify service file no longer exists
		serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
		cmd = fmt.Sprintf("sudo test -f %s && echo 'exists' || echo 'not found'", serviceFilePath)
		fileCheckOutput := runInVM(t, QuicDeleteVM, cmd)
		require.Contains(t, fileCheckOutput, "not found", "systemd service file should not exist after delete")
	})

	t.Run("ValidateFirewallPortClosed", func(t *testing.T) {
		// Extract port from the checkout output connection string
		connectionString := strings.TrimSpace(checkoutOutput)
		parts := strings.Split(connectionString, ":")
		require.True(t, len(parts) >= 3, "connection string should have port")
		portPart := strings.Split(parts[len(parts)-1], "/")[0]

		// Verify UFW rule was removed for the port
		ufwOutput := runInVM(t, QuicDeleteVM, "sudo ufw status")
		portRule := fmt.Sprintf("%s/tcp", portPart)
		require.NotContains(t, ufwOutput, portRule, "UFW should not contain rule for checkout port after delete")
	})

	t.Run("ValidateCloneDirectoryRemoved", func(t *testing.T) {
		clonePath := fmt.Sprintf("/opt/quic/%s/%s", templateName, branchName)

		// Verify clone directory no longer exists
		cmd := fmt.Sprintf("sudo test -d %s && echo 'exists' || echo 'not found'", clonePath)
		dirCheckOutput := runInVM(t, QuicDeleteVM, cmd)
		require.Contains(t, dirCheckOutput, "not found", "clone directory should not exist after delete")
	})

	t.Run("DeleteNonExistentBranch", func(t *testing.T) {
		nonExistentBranch := "non-existent-branch"

		// Delete a non-existent branch should not error but should indicate nothing was deleted
		deleteOutput, err := runQuic(t, "delete", nonExistentBranch)
		require.NoError(t, err, "delete non-existent branch should not error")
		// The exact behavior may vary, but it shouldn't crash
		t.Logf("Delete non-existent branch output: %s", deleteOutput)
	})
}
