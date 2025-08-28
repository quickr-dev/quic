package e2e

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/quickr-dev/quic/proto"
)

func TestCheckoutFlow(t *testing.T) {
	// Setup gRPC client
	client, ctx, cancel := setupGRPCClient(t)
	defer cancel()

	t.Run("RestoreDatasetInitialState", func(t *testing.T) {
		// Verify restore dataset exists and has expected standby files
		require.True(t, datasetExistsInVM(t, "tank/_restore"),
			"Restore dataset must exist for e2e tests")

		// Get restore dataset mountpoint
		restorePath, err := execInVMSudo(t, "zfs", "get", "-H", "-o", "value", "mountpoint", "tank/_restore")
		require.NoError(t, err)

		// Verify restore dataset has standby configuration (source state)
		standbySignalPath := filepath.Join(restorePath, "standby.signal")
		assertFileExists(t, standbySignalPath)

		// Verify restore has recovery configuration in postgresql.auto.conf
		autoConfPath := filepath.Join(restorePath, "postgresql.auto.conf")
		if assertFileExists(t, autoConfPath) {
			// Check it contains recovery settings (not our clone comment)
			assertFileDoesNotContain(t, autoConfPath, "# Clone instance - recovery disabled")
		}
	})

	t.Run("CreateNewCheckouta", func(t *testing.T) {
		cloneName := "pr-" + randomString(6)

		// Verify restore dataset exists
		require.True(t, datasetExistsInVM(t, "tank/_restore"),
			"Restore dataset must exist for e2e tests")

		// Capture UFW status BEFORE checkout
		ufwStatusBefore := getUFWStatus(t)

		// Verify clone dataset doesn't exist yet
		cloneDataset := "tank/" + cloneName
		assert.False(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should not exist before checkout")

		resp, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.ConnectionString)

		// Capture UFW status AFTER checkout
		ufwStatusAfter := getUFWStatus(t)

		// Extract info from connection string
		connStr := resp.ConnectionString
		assert.Contains(t, connStr, "postgres://admin:")
		assert.Contains(t, connStr, "@localhost:")
		assert.Contains(t, connStr, "/postgres?sslmode=disable")

		// Parse port and password from connection string for validation
		port, adminPassword, err := parseConnectionString(connStr)
		require.NoError(t, err, "Should be able to extract port and password from connection string")
		assert.Greater(t, port, 0)
		assert.NotEmpty(t, adminPassword)

		// Set clonePath for further validation
		clonePath := "/tank/" + cloneName

		// Verify ZFS operations actually happened
		snapshotName := "tank/_restore@" + cloneName
		assert.True(t, snapshotExistsInVM(t, snapshotName), "Snapshot should exist")

		assert.True(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should exist")

		// Verify mountpoint exists and is accessible
		assertDirExists(t, clonePath)

		// Verify permissions
		assertPostgresOwnership(t, "/tank/_restore")
		assertPostgresOwnership(t, clonePath)

		// Verify PostgreSQL port is open
		assertPortInUse(t, port)

		// Verify standby.signal is removed
		standbySignalPath := filepath.Join(clonePath, "standby.signal")
		assertFileNotExists(t, standbySignalPath)

		// Verify postgresql.auto.conf is cleaned
		autoConfPath := filepath.Join(clonePath, "postgresql.auto.conf")
		assertFileContains(t, autoConfPath, "# Clone instance - recovery and archiving disabled")
		assertFileContains(t, autoConfPath, "archive_mode = 'off'")
		assertFileContains(t, autoConfPath, "restore_command = ''")

		// Verify external access configuration
		postgresqlConfPath := filepath.Join(clonePath, "postgresql.conf")
		assertFileContains(t, postgresqlConfPath, "listen_addresses = '*'")

		// Verify complete postgresql.conf configuration
		assertFileContains(t, postgresqlConfPath, "max_connections = 10")
		assertFileContains(t, postgresqlConfPath, "wal_level = minimal")
		assertFileContains(t, postgresqlConfPath, "max_wal_senders = 0")
		assertFileContains(t, postgresqlConfPath, "archive_mode = off")
		assertFileContains(t, postgresqlConfPath, "max_wal_size = '64MB'")
		assertFileContains(t, postgresqlConfPath, "maintenance_work_mem = '16MB'")
		assertFileContains(t, postgresqlConfPath, "effective_cache_size = '128MB'")
		assertFileContains(t, postgresqlConfPath, "shared_buffers = '32MB'")
		assertFileContains(t, postgresqlConfPath, "work_mem = '4MB'")
		assertFileContains(t, postgresqlConfPath, "random_page_cost = 1.1")
		assertFileContains(t, postgresqlConfPath, "default_statistics_target = 100")
		assertFileContains(t, postgresqlConfPath, "max_worker_processes = 2")
		assertFileContains(t, postgresqlConfPath, "max_parallel_workers = 1")
		assertFileContains(t, postgresqlConfPath, "max_parallel_workers_per_gather = 1")
		assertFileContains(t, postgresqlConfPath, "synchronous_commit = off")

		pgHbaConfPath := filepath.Join(clonePath, "pg_hba.conf")
		assertFileContains(t, pgHbaConfPath, "host    all             admin           0.0.0.0/0               md5")

		// Verify admin user exists and can connect
		assertAdminUserCanConnect(t, port, adminPassword)

		// Verify PostgreSQL process is running for this specific clone
		assertPostgresProcessRunning(t, clonePath)

		// Check AFTER state - verify firewall rule was added for the checkout port
		portStr := fmt.Sprintf("%d/tcp", port)
		assert.NotContains(t, ufwStatusBefore, portStr,
			"UFW status before checkout should not contain port %d", port)
		assert.Contains(t, ufwStatusAfter, portStr,
			"UFW status after checkout should contain port %d", port)

		// Verify the clone dataset now exists
		assert.True(t, datasetExistsInVM(t, cloneDataset), "Clone dataset should exist after checkout")

		// Verify clone is different from restore - standby.signal should be removed from clone
		standbySignalPathClone := filepath.Join(clonePath, "standby.signal")
		assertFileNotExists(t, standbySignalPathClone)

		// Verify clone has different configuration than restore
		postgresqlConfPathClone := filepath.Join(clonePath, "postgresql.conf")
		assertFileContains(t, postgresqlConfPathClone, "listen_addresses = '*'")
		assertFileContains(t, postgresqlConfPathClone, "max_connections = 10")
		assertFileContains(t, postgresqlConfPathClone, "synchronous_commit = off")

		pgHbaConfPathClone := filepath.Join(clonePath, "pg_hba.conf")
		assertFileContains(t, pgHbaConfPathClone, "host    all             admin           0.0.0.0/0               md5")
	})

	t.Run("DuplicateCheckout", func(t *testing.T) {
		cloneName := "duplicate-" + randomString(6) // Use random name to avoid conflicts

		// Create first time
		resp1, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp1.ConnectionString)

		// Create second time - should return existing
		resp2, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp2.ConnectionString)

		// Test connection strings are identical
		assert.Equal(t, resp1.ConnectionString, resp2.ConnectionString)

		// Parse connection info for validation
		port, adminPassword, err := parseConnectionString(resp2.ConnectionString)
		require.NoError(t, err)

		// Verify the duplicate checkout is also properly configured
		assertAdminUserCanConnect(t, port, adminPassword)
	})

	t.Run("InvalidCloneName", func(t *testing.T) {
		// Test invalid clone name - reserved name
		_, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: "_restore",
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid clone name")
	})

	t.Run("CheckoutProperlyConfigured", func(t *testing.T) {
		// This test specifically focuses on the configuration aspects
		cloneName := "config-test-" + randomString(6)

		resp, err := client.CreateCheckout(ctx, &pb.CreateCheckoutRequest{
			CloneName: cloneName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, resp.ConnectionString)

		// Test that the connection string works with the admin user
		// Replace localhost with VM IP for testing from host
		connStr := resp.ConnectionString
		vmIP := getVMIP(t)
		actualConnStr := strings.Replace(connStr, "@localhost:", fmt.Sprintf("@%s:", vmIP), 1)
		db, err := sql.Open("postgres", actualConnStr)
		require.NoError(t, err)
		defer db.Close()

		// Should be able to connect and query
		var result int
		err = db.QueryRow("SELECT 1").Scan(&result)
		assert.NoError(t, err)
		assert.Equal(t, 1, result)

		// Test admin user privileges - should be able to create databases
		_, err = db.Exec("CREATE DATABASE test_admin_db")
		assert.NoError(t, err, "Admin user should have CREATE DATABASE privileges")

		// Clean up
		_, err = db.Exec("DROP DATABASE test_admin_db")
		assert.NoError(t, err)

		// Parse connection info for validation
		port, _, err := parseConnectionString(connStr)
		require.NoError(t, err)

		// Verify the clone is properly isolated - check it has its own data directory
		clonePath := "/tank/" + cloneName
		assertDirExists(t, clonePath)

		// Verify clone metadata is saved and readable
		metadataPath := filepath.Join(clonePath, ".quic-meta.json")
		assertFileExists(t, metadataPath)
		assertFileContains(t, metadataPath, fmt.Sprintf(`"port": %d`, port))
	})
}
