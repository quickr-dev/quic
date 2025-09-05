package e2e_agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/quickr-dev/quic/internal/agent"
)

func TestDeleteFlow(t *testing.T) {
	// Setup shared restore dataset for all tests
	service, restoreResult := createRestore(t)

	t.Run("DeleteZFSSnapshot", func(t *testing.T) {
		cloneName := generateCloneName()
		restoreDatasetName := fmt.Sprintf("tank/%s", restoreResult.Dirname)
		snapshotName := fmt.Sprintf("%s@%s", restoreDatasetName, cloneName)

		// Create a checkout (creates snapshot and clone)
		checkoutResult, err := service.CreateBranch(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify snapshot exists before deletion
		verifyZFSDatasetExists(t, snapshotName, true)

		// Delete the checkout
		deleted, err := service.DeleteBranch(context.Background(), cloneName, restoreResult.Dirname)
		require.NoError(t, err, "DeleteCheckout should succeed")
		require.True(t, deleted, "DeleteCheckout should return true when checkout was deleted")

		// Verify snapshot no longer exists
		verifyZFSDatasetExists(t, snapshotName, false)
	})

	t.Run("DeleteZFSClone", func(t *testing.T) {
		cloneName := generateCloneName()
		cloneDatasetName := fmt.Sprintf("tank/%s/%s", restoreResult.Dirname, cloneName)

		// Create a checkout (creates snapshot and clone)
		checkoutResult, err := service.CreateBranch(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify clone dataset exists before deletion
		verifyZFSDatasetExists(t, cloneDatasetName, true)

		// Delete the checkout
		deleted, err := service.DeleteBranch(context.Background(), cloneName, restoreResult.Dirname)
		require.NoError(t, err, "DeleteCheckout should succeed")
		require.True(t, deleted, "DeleteCheckout should return true when checkout was deleted")

		// Verify clone dataset no longer exists
		verifyZFSDatasetExists(t, cloneDatasetName, false)
	})

	t.Run("RemoveSystemdService", func(t *testing.T) {
		cloneName := generateCloneName()
		serviceName := fmt.Sprintf("quic-clone-%s", cloneName)

		// Create a checkout (creates systemd service)
		checkoutResult, err := service.CreateBranch(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify systemd service is running and file exists
		assertSystemdServiceRunning(t, serviceName, true)
		verifySystemdFileExists(t, serviceName, true)

		// Delete the checkout
		deleted, err := service.DeleteBranch(context.Background(), cloneName, restoreResult.Dirname)
		require.NoError(t, err, "DeleteCheckout should succeed")
		require.True(t, deleted, "DeleteCheckout should return true when checkout was deleted")

		// Verify systemd service is not running and file was deleted
		assertSystemdServiceRunning(t, serviceName, false)
		verifySystemdFileExists(t, serviceName, false)
	})

	t.Run("CloseFirewallPort", func(t *testing.T) {
		cloneName := generateCloneName()

		// Create a checkout (opens firewall port)
		checkoutResult, err := service.CreateBranch(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify UFW contains rule for the port
		assertUFWTcp(t, checkoutResult.Port, true)

		// Delete the checkout
		deleted, err := service.DeleteBranch(context.Background(), cloneName, restoreResult.Dirname)
		require.NoError(t, err, "DeleteCheckout should succeed")
		require.True(t, deleted, "DeleteCheckout should return true when checkout was deleted")

		// Verify UFW no longer contains rule for the port
		assertUFWTcp(t, checkoutResult.Port, false)
	})

	t.Run("AuditLogEntry", func(t *testing.T) {
		cloneName := generateCloneName()

		// Create a checkout
		_, err := service.CreateBranch(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")

		// Delete the checkout
		deleted, err := service.DeleteBranch(context.Background(), cloneName, restoreResult.Dirname)
		require.NoError(t, err, "DeleteCheckout should succeed")
		require.True(t, deleted, "DeleteCheckout should return true when checkout was deleted")

		// Read the last line of the audit log and verify it's a delete entry
		cmd := exec.Command("tail", "-n", "1", agent.LogFile)
		output, err := cmd.Output()
		require.NoError(t, err, "Should be able to read last line of audit log")

		auditEntry, err := agent.ParseAuditEntry(strings.TrimSpace(string(output)))
		require.NoError(t, err, "Should be able to parse audit log entry")
		require.Equal(t, "checkout_delete", auditEntry["event_type"], "Event type should be checkout_delete")
	})

	t.Run("DeleteNonExistentCheckout", func(t *testing.T) {
		nonExistentCloneName := generateCloneName()

		// Delete a non-existent checkout
		deleted, err := service.DeleteBranch(context.Background(), nonExistentCloneName, restoreResult.Dirname)
		require.NoError(t, err, "DeleteCheckout should not return error for non-existent checkout")
		require.False(t, deleted, "DeleteCheckout should return false when nothing was deleted")
	})

	t.Run("InvalidCloneName", func(t *testing.T) {
		// Test reserved name "_restore"
		_, err := service.DeleteBranch(context.Background(), "_restore", restoreResult.Dirname)
		require.Error(t, err, "Should reject reserved name '_restore'")
		require.Equal(t, "invalid clone name: clone name '_restore' is reserved", err.Error())

		// Test invalid characters
		_, err = service.DeleteBranch(context.Background(), "test@invalid", restoreResult.Dirname)
		require.Error(t, err, "Should reject names with invalid characters")
		require.Equal(t, "invalid clone name: clone name must contain only letters, numbers, underscore, and dash", err.Error())

		// Test empty name
		_, err = service.DeleteBranch(context.Background(), "", restoreResult.Dirname)
		require.Error(t, err, "Should reject empty name")
		require.Equal(t, "invalid clone name: clone name must be between 1 and 50 characters", err.Error())

		// Test name too long (over 50 characters)
		longName := strings.Repeat("a", 51)
		_, err = service.DeleteBranch(context.Background(), longName, restoreResult.Dirname)
		require.Error(t, err, "Should reject names longer than 50 characters")
		require.Equal(t, "invalid clone name: clone name must be between 1 and 50 characters", err.Error())
	})
}
