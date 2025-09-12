package e2e_cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/quickr-dev/quic/internal/agent"
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
		cloneDatasetName := agent.GetBranchDataset(templateName, branchName)

		output := runInVM(t, QuicDeleteVM, "zfs list")
		require.NotContains(t, output, cloneDatasetName)
	})

	t.Run("ValidateZFSSnapshotDeleted", func(t *testing.T) {
		snapshotName := agent.GetSnapshotName(templateName, branchName)

		output := runInVM(t, QuicDeleteVM, "zfs list -t snapshot")
		require.NotContains(t, output, snapshotName)
	})

	t.Run("ValidateSystemdServiceRemoved", func(t *testing.T) {
		serviceName := agent.GetBranchServiceName(templateName, branchName)

		// Verify systemd service is not running
		cmd := fmt.Sprintf("sudo systemctl is-active %s 2>/dev/null || echo 'inactive'", serviceName)
		serviceStatusOutput := runInVM(t, QuicDeleteVM, cmd)
		require.Contains(t, serviceStatusOutput, "inactive", "PostgreSQL clone service should not be active after delete")

		// Verify service file no longer exists
		serviceFilePath := agent.GetServiceFilePath(serviceName)
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
		branchMountpoint := agent.GetBranchMountpoint(templateName, branchName)

		// Verify clone directory no longer exists
		cmd := fmt.Sprintf("sudo test -d %s && echo 'exists' || echo 'not found'", branchMountpoint)
		dirCheckOutput := runInVM(t, QuicDeleteVM, cmd)
		require.Contains(t, dirCheckOutput, "not found", "clone directory should not exist after delete")
	})

	t.Run("ValidateAuditLogEntry", func(t *testing.T) {
		cmd := fmt.Sprintf("sudo tail -n 1 %s", agent.AuditFile)
		output := runInVM(t, QuicDeleteVM, cmd)

		auditEntry, err := agent.ParseAuditEntry(strings.TrimSpace(output))
		require.NoError(t, err, "Should be able to parse audit log entry")
		require.Equal(t, "branch_delete", auditEntry["event_type"], "Event type should be checkout_delete")
	})

	t.Run("DeleteNonExistentBranch", func(t *testing.T) {
		deleteOutput, err := runQuic(t, "delete", "non-existent-branch")
		require.NoError(t, err, deleteOutput)
		require.Equal(t, deleteOutput, "")
	})
}

func TestQuicDeleteInvalidBranchNames(t *testing.T) {
	t.Run("ReservedName", func(t *testing.T) {
		output, err := runQuic(t, "delete", "_restore")
		require.Error(t, err, "Should reject reserved name '_restore'")
		require.Contains(t, output, "branch name '_restore' is reserved")
	})

	t.Run("InvalidCharacters", func(t *testing.T) {
		output, err := runQuic(t, "delete", "test@invalid")
		require.Error(t, err, "Should reject names with invalid characters")
		require.Contains(t, output, "branch name must contain only letters, numbers, underscore, and dash")
	})

	t.Run("EmptyName", func(t *testing.T) {
		output, err := runQuic(t, "delete", "")
		require.Error(t, err, "Should reject empty name")
		require.Contains(t, output, "branch name must be between 1 and 50 characters")
	})

	t.Run("NameTooLong", func(t *testing.T) {
		longName := strings.Repeat("a", 51)
		output, err := runQuic(t, "delete", longName)
		require.Error(t, err, "Should reject names longer than 50 characters")
		require.Contains(t, output, "branch name must be between 1 and 50 characters")
	})
}
