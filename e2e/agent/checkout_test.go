package e2e_agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/quickr-dev/quic/internal/agent"
)

var sharedRestoreResult *agent.InitResult
var sharedCheckoutService *agent.CheckoutService

func runQuicdInit(t *testing.T) (*agent.CheckoutService, *agent.InitResult) {
	if sharedRestoreResult != nil && sharedCheckoutService != nil {
		return sharedCheckoutService, sharedRestoreResult
	}

	// Create a unique dirname for the shared restore
	testDirname := fmt.Sprintf("shared-restore-%d", time.Now().Unix())

	// Create checkout service with test config
	config := &agent.CheckoutConfig{
		ZFSParentDataset: "tank",
		PostgresBinPath:  "/usr/lib/postgresql/16/bin",
		StartPort:        5433,
		EndPort:          6433,
	}
	service := agent.NewCheckoutService(config)

	// Perform init operation to create restore dataset
	initConfig := &agent.InitConfig{
		Stanza:   testStanza,
		Database: testDatabase,
		Dirname:  testDirname,
	}

	result, err := service.PerformInit(initConfig)
	require.NoError(t, err, "Shared restore init should succeed")
	require.NotNil(t, result)

	// Store for reuse
	sharedCheckoutService = service
	sharedRestoreResult = result

	return service, result
}

func TestCheckoutFlow(t *testing.T) {
	// Setup shared restore dataset for all tests
	service, restoreResult := runQuicdInit(t)

	t.Run("CreateZFSSnapshot", func(t *testing.T) {
		cloneName := generateCloneName()
		restoreDatasetName := fmt.Sprintf("tank/%s", restoreResult.Dirname)
		snapshotName := fmt.Sprintf("%s@%s", restoreDatasetName, cloneName)

		// Verify snapshot doesn't exist before
		verifyZFSDatasetExists(t, snapshotName, false)

		// create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify snapshot was created
		verifyZFSDatasetExists(t, snapshotName, true)
	})

	t.Run("CreateZFSClone", func(t *testing.T) {
		cloneName := generateCloneName()
		cloneDatasetName := fmt.Sprintf("tank/%s/%s", restoreResult.Dirname, cloneName)

		// Verify clone doesn't exist before
		verifyZFSDatasetExists(t, cloneDatasetName, false)

		// Create checkout (which internally creates snapshot and clone)
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify clone dataset was created
		verifyZFSDatasetExists(t, cloneDatasetName, true)

		// Verify clone has correct mountpoint
		expectedMountpoint := fmt.Sprintf("/opt/quic/%s/%s", restoreResult.Dirname, cloneName)
		verifyZFSMountpoint(t, cloneDatasetName, expectedMountpoint)

		// Verify clone path matches checkout result
		require.Equal(t, expectedMountpoint, checkoutResult.ClonePath, "CheckoutResult ClonePath should match ZFS mountpoint")
	})

	t.Run("ConfigureCloneForCheckout", func(t *testing.T) {
		cloneName := generateCloneName()

		// Verify pre-checkout state in the restore dataset
		restorePath := getRestorePath(restoreResult.Dirname)

		// Verify postmaster.pid points to restore directory before checkout
		restorePidData := parsePostmasterPid(t, restorePath)
		require.Equal(t, restorePath, restorePidData["dataDirectory"], "postmaster.pid should initially point to restore directory")
		restorePort := restorePidData["port"].(int)

		// Verify restore port is > 5000
		require.Greater(t, restorePort, 5000, "restore port should be greater than 5000")

		// Ensure files exist before checkout
		verifyFileExists(t, restorePath+"/standby.signal", true)
		touch(t, restorePath+"/recovery.signal")
		touch(t, restorePath+"/recovery.conf")
		verifyFileExists(t, restorePath+"/recovery.signal", true)
		verifyFileExists(t, restorePath+"/recovery.conf", true)

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		clonePath := checkoutResult.ClonePath

		// Verify standby and recovery files are removed
		verifyFileExists(t, clonePath+"/standby.signal", false)
		verifyFileExists(t, clonePath+"/recovery.signal", false)
		verifyFileExists(t, clonePath+"/recovery.conf", false)

		// Verify postmaster.pid exists and contains correct information for this clone
		pidData := parsePostmasterPid(t, clonePath)
		require.Equal(t, clonePath, pidData["dataDirectory"], "postmaster.pid should contain correct data directory")
		clonePort := pidData["port"].(int)
		require.Equal(t, checkoutResult.Port, clonePort, "postmaster.pid should contain correct port")

		// Verify clone port is different from restore port
		require.NotEqual(t, restorePort, clonePort, "clone port should be different from restore port")

		// Verify postgresql.auto.conf is configured for clone
		autoConfPath := clonePath + "/postgresql.auto.conf"
		verifyFileExists(t, autoConfPath, true)
		verifyFileContains(t, autoConfPath, "# Clone instance", "should contain clone identifier")
		verifyFileContains(t, autoConfPath, "archive_mode = 'off'", "should disable archiving")
		verifyFileContains(t, autoConfPath, "restore_command = ''", "should clear restore command")

		// Verify postgresql.conf is configured with clone-specific settings
		postgresqlConfPath := clonePath + "/postgresql.conf"
		verifyFileContains(t, postgresqlConfPath, "listen_addresses = '*'", "should allow external connections")
		verifyFileContains(t, postgresqlConfPath, "max_connections = 5", "should have clone-optimized connection limit")
		verifyFileContains(t, postgresqlConfPath, "wal_level = minimal", "should use minimal WAL level")
		verifyFileContains(t, postgresqlConfPath, "synchronous_commit = off", "should disable synchronous commit")

		// Verify pg_hba.conf allows admin user access
		pgHbaConfPath := clonePath + "/pg_hba.conf"
		verifyFileContains(t, pgHbaConfPath, "host    all             admin           0.0.0.0/0               md5", "should allow admin user from any IP")

		// Verify WAL directory exists and has proper permissions
		walDirPath := clonePath + "/pg_wal"
		verifyDirectoryExists(t, walDirPath)
		verifyDirectoryPermissions(t, walDirPath, "700")
	})

	t.Run("StartPostgreSQLService", func(t *testing.T) {
		cloneName := generateCloneName()

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		clonePath := checkoutResult.ClonePath

		// Verify systemd service was created and is running
		serviceName := fmt.Sprintf("quic-clone-%s", cloneName)
		assertSystemdServiceRunning(t, serviceName)

		// Verify postmaster.pid exists and contains running process
		assertCloneInstanceRunning(t, clonePath)

		// Verify PostgreSQL process is bound to correct data directory
		pidData := parsePostmasterPid(t, clonePath)
		require.Equal(t, clonePath, pidData["dataDirectory"], "PostgreSQL should use the clone data directory")
	})

	t.Run("CloneConnectivitySpeed", func(t *testing.T) {
		startTime := time.Now()
		cloneName := generateCloneName()

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Assert users can quickly use the cloned instance
		recoveryOutput, err := service.ExecPostgresCommand(checkoutResult.Port, "postgres", "SELECT pg_is_in_recovery();")
		require.NoError(t, err, "Error checking recovery status")
		require.Contains(t, recoveryOutput, "f", "PostgreSQL should not be in recovery mode (pg_is_in_recovery should return 'f')")

		// Make a real query to the cloned instance
		usersOutput, err := service.ExecPostgresCommand(checkoutResult.Port, "testdb", "SELECT name FROM users ORDER BY name;")
		require.NoError(t, err, "Should be able to query test data from restored database")
		require.Equal(t, usersOutput, "  name   \n---------\n Alice\n Bob\n Charlie\n(3 rows)\n\n")

		totalTime := time.Since(startTime)
		require.Less(t, totalTime, 5*time.Second, "Branch should be ready within 5 seconds, took %v", totalTime)
	})

	t.Run("CreateAdminUser", func(t *testing.T) {
		cloneName := generateCloneName()

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")

		connStr := checkoutResult.ConnectionString("localhost")
		require.Contains(t, connStr, "postgresql://admin:", "Connection string should contain admin user")

		// Test that admin is super user
		output, err := service.ExecPostgresCommand(checkoutResult.Port, "postgres", "SELECT rolname, rolsuper FROM pg_roles WHERE rolname = 'admin';")
		require.NoError(t, err, "Should be able to query admin user privileges")
		require.Equal(t, output, " rolname | rolsuper \n---------+----------\n admin   | t\n(1 row)\n\n", "Admin should be super user")
	})

	t.Run("ConfigureFirewall", func(t *testing.T) {
		cloneName := generateCloneName()

		ufwBefore := getUFWStatus(t)

		// Create checkout (gets available port dynamically)
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")

		ufwAfter := getUFWStatus(t)

		// Verify firewall rule was not present before checkout
		portStr := fmt.Sprintf("%d/tcp", checkoutResult.Port)
		require.NotContains(t, ufwBefore, portStr, "UFW should not contain port %d before checkout", checkoutResult.Port)

		// Verify firewall rule was added for the checkout port
		require.Contains(t, ufwAfter, portStr, "UFW should contain port %d after checkout", checkoutResult.Port)
	})

	t.Run("DuplicateCheckoutReturnsExisting", func(t *testing.T) {
		// Test that creating the same checkout twice returns the existing one
		t.Skip("Not yet implemented")
	})

	t.Run("InvalidCloneNameRejected", func(t *testing.T) {
		// Test that invalid clone names (like "_restore") are rejected
		t.Skip("Not yet implemented")
	})
}
