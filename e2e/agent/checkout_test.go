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
		require.Equal(t, fmt.Sprintf("%d", checkoutResult.Port), pidData["port"], "postmaster.pid should contain correct port")

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
		// Test starting PostgreSQL service on clone
		t.Skip("Not yet implemented")
	})

	t.Run("CreateAdminUser", func(t *testing.T) {
		// Test creating admin user with random password
		t.Skip("Not yet implemented")
	})

	t.Run("ConfigureFirewall", func(t *testing.T) {
		// Test opening firewall port for external access
		t.Skip("Not yet implemented")
	})

	t.Run("VerifyCheckoutConnectivity", func(t *testing.T) {
		// Test that we can connect to the checkout database externally
		t.Skip("Not yet implemented")
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
