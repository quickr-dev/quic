package e2e

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/quickr-dev/quic/proto"
)

func TestDeleteFlow(t *testing.T) {
	// Setup gRPC client
	client, ctx, cancel := setupGRPCClient(t)
	defer cancel()

	t.Run("DeleteExistingCheckout", func(t *testing.T) {
		cloneName := "delete-test-" + randomString(6)

		// Verify restore dataset exists
		require.True(t, datasetExistsInVM(t, "tank/_restore"),
			"Restore dataset must exist for e2e tests")

		// 1. Create a checkout first
		resp, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.ConnectionString)

		// Extract checkout info from connection string for validation
		connStr := resp.ConnectionString
		assert.Contains(t, connStr, "postgres://admin:")
		assert.Contains(t, connStr, "@localhost:")
		assert.Contains(t, connStr, "/postgres?sslmode=disable")

		// Parse port from connection string for validation
		port, _, err := parseConnectionString(connStr)
		require.NoError(t, err, "Should be able to extract port from connection string")

		// Capture state BEFORE deletion
		cloneDataset := "tank/" + cloneName
		snapshotName := "tank/_restore@" + cloneName
		clonePath := "/tank/" + cloneName
		metadataPath := filepath.Join(clonePath, ".quic-meta.json")
		ufwStatusBefore := getUFWStatus(t)

		// Verify checkout is properly set up
		assert.True(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should exist before delete")
		assert.True(t, snapshotExistsInVM(t, snapshotName), "Snapshot should exist before delete")
		assertDirExists(t, clonePath)
		assertFileExists(t, metadataPath)
		assertPostgresProcessRunning(t, clonePath)
		assertPortInUse(t, port)

		// Verify UFW rule exists for the port
		portStr := fmt.Sprintf("%d/tcp", port)
		assert.Contains(t, ufwStatusBefore, portStr,
			"UFW should contain rule for port %d before delete", port)

		// 2. Delete the checkout
		deleteResp, err := client.DeleteCheckout(ctx, &pb.DeleteCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		assert.True(t, deleteResp.Deleted, "Delete should return true for existing checkout")

		// Capture state AFTER deletion
		ufwStatusAfter := getUFWStatus(t)

		// 3. Verify cleanup happened
		// ZFS resources should be gone
		assert.False(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should not exist after delete")
		assert.False(t, snapshotExistsInVM(t, snapshotName), "Snapshot should not exist after delete")

		// Directory should not be accessible (mountpoint gone)
		assertDirNotExists(t, clonePath)

		// Metadata file should be gone
		assertFileNotExists(t, metadataPath)

		// PostgreSQL process should be stopped
		assertPostgresProcessNotRunning(t, clonePath)

		// Port should be free
		assertPortNotInUse(t, port)

		// UFW rule should be removed
		assert.NotContains(t, ufwStatusAfter, portStr,
			"UFW should not contain rule for port %d after delete", port)
	})

	t.Run("DeleteNonExistentCheckout", func(t *testing.T) {
		cloneName := "nonexistent-" + randomString(6)

		// Try to delete a checkout that doesn't exist
		deleteResp, err := client.DeleteCheckout(ctx, &pb.DeleteCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		assert.False(t, deleteResp.Deleted, "Delete should return false for non-existent checkout")
	})

	t.Run("DeleteIdempotency", func(t *testing.T) {
		cloneName := "idempotent-test-" + randomString(6)

		// Create a checkout
		resp, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.ConnectionString)

		cloneDataset := "tank/" + cloneName
		snapshotName := "tank/_restore@" + cloneName

		// Verify it exists
		assert.True(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should exist")
		assert.True(t, snapshotExistsInVM(t, snapshotName), "Snapshot should exist")

		// Delete first time
		deleteResp1, err := client.DeleteCheckout(ctx, &pb.DeleteCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		assert.True(t, deleteResp1.Deleted, "First delete should return true")

		// Verify it's gone
		assert.False(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should not exist after first delete")
		assert.False(t, snapshotExistsInVM(t, snapshotName), "Snapshot should not exist after first delete")

		// Delete second time (idempotency test)
		deleteResp2, err := client.DeleteCheckout(ctx, &pb.DeleteCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		assert.False(t, deleteResp2.Deleted, "Second delete should return false (nothing to delete)")

		// Still should be gone
		assert.False(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should still not exist after second delete")
		assert.False(t, snapshotExistsInVM(t, snapshotName), "Snapshot should still not exist after second delete")
	})

	t.Run("DeleteWithInvalidDatabase", func(t *testing.T) {
		cloneName := "invalid-db-test-" + randomString(6)

		// Try to delete a checkout that doesn't exist
		deleteResp, err := client.DeleteCheckout(ctx, &pb.DeleteCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		assert.False(t, deleteResp.Deleted, "Delete should return false for non-existent checkout")
	})

	t.Run("DeletePartialCleanupResilience", func(t *testing.T) {
		cloneName := "partial-cleanup-" + randomString(6)

		// Create a checkout
		resp, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.ConnectionString)

		cloneDataset := "tank/" + cloneName
		snapshotName := "tank/_restore@" + cloneName
		clonePath := "/tank/" + cloneName
		metadataPath := filepath.Join(clonePath, ".quic-meta.json")

		// Verify it exists
		assert.True(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should exist")
		assert.True(t, snapshotExistsInVM(t, snapshotName), "Snapshot should exist")
		assertFileExists(t, metadataPath)

		// Manually remove metadata file to simulate partial state
		execInVMSudo(t, "rm", "-f", metadataPath)
		assertFileNotExists(t, metadataPath)

		// Delete should still work even with missing metadata
		deleteResp, err := client.DeleteCheckout(ctx, &pb.DeleteCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		assert.True(t, deleteResp.Deleted, "Delete should handle missing metadata gracefully")

		// Everything should still be cleaned up
		assert.False(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should not exist after delete")
		assert.False(t, snapshotExistsInVM(t, snapshotName), "Snapshot should not exist after delete")
	})
}
