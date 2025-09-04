package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
		cloneName := fmt.Sprintf("test-clone-%d", time.Now().Unix())
		restoreDatasetName := fmt.Sprintf("tank/%s", restoreResult.Dirname)
		snapshotName := fmt.Sprintf("%s@%s", restoreDatasetName, cloneName)

		// Verify snapshot doesn't exist before
		cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", snapshotName)
		require.Error(t, cmd.Run(), "Snapshot should not exist before creation")

		// create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify snapshot was created
		cmd = exec.Command("sudo", "zfs", "list", "-H", "-o", "name", snapshotName)
		err = cmd.Run()
		require.NoError(t, err, "Snapshot should exist after creation")
	})

	t.Run("CreateZFSClone", func(t *testing.T) {
		cloneName := fmt.Sprintf("test-clone-%d", time.Now().Unix())
		cloneDatasetName := fmt.Sprintf("tank/%s/%s", restoreResult.Dirname, cloneName)

		// Verify clone doesn't exist before
		cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", cloneDatasetName)
		require.Error(t, cmd.Run(), "Clone dataset should not exist before creation")

		// Create checkout (which internally creates snapshot and clone)
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		// Verify clone dataset was created
		cmd = exec.Command("sudo", "zfs", "list", "-H", "-o", "name", cloneDatasetName)
		err = cmd.Run()
		require.NoError(t, err, "Clone dataset should exist after checkout creation")

		// Verify clone has correct mountpoint
		cmd = exec.Command("sudo", "zfs", "get", "-H", "-o", "value", "mountpoint", cloneDatasetName)
		output, err := cmd.Output()
		require.NoError(t, err, "Should be able to get clone mountpoint")

		mountpoint := strings.TrimSpace(string(output))
		expectedMountpoint := fmt.Sprintf("/opt/quic/%s/%s", restoreResult.Dirname, cloneName)
		require.Equal(t, expectedMountpoint, mountpoint, "Clone should have expected mountpoint")

		// Verify clone path matches checkout result
		require.Equal(t, expectedMountpoint, checkoutResult.ClonePath, "CheckoutResult ClonePath should match ZFS mountpoint")
	})

	t.Run("ConfigureCloneForCheckout", func(t *testing.T) {
		cloneName := fmt.Sprintf("test-clone-%d", time.Now().Unix())

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, "e2e-test")
		require.NoError(t, err, "CreateCheckout should succeed")
		require.NotNil(t, checkoutResult, "CreateCheckout should return result")

		clonePath := checkoutResult.ClonePath

		// Verify standby and recovery files are removed
		standbySignalPath := clonePath + "/standby.signal"
		cmd := exec.Command("sudo", "test", "-f", standbySignalPath)
		require.Error(t, cmd.Run(), "standby.signal should be removed from clone")

		recoverySignalPath := clonePath + "/recovery.signal"
		cmd = exec.Command("sudo", "test", "-f", recoverySignalPath)
		require.Error(t, cmd.Run(), "recovery.signal should be removed from clone")

		recoveryConfPath := clonePath + "/recovery.conf"
		cmd = exec.Command("sudo", "test", "-f", recoveryConfPath)
		require.Error(t, cmd.Run(), "recovery.conf should be removed from clone")

		// Verify postmaster.pid exists and contains correct information for this clone
		postmasterPidPath := clonePath + "/postmaster.pid"
		cmd = exec.Command("sudo", "test", "-f", postmasterPidPath)
		require.NoError(t, cmd.Run(), "postmaster.pid should exist for running PostgreSQL")

		// Read postmaster.pid and verify it contains the correct port and data directory
		cmd = exec.Command("sudo", "cat", postmasterPidPath)
		output, err := cmd.Output()
		require.NoError(t, err, "Should be able to read postmaster.pid")

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		require.GreaterOrEqual(t, len(lines), 4, "postmaster.pid should have at least 4 lines")

		// Second line contains the data directory path
		if len(lines) >= 2 {
			dataDirectoryLine := strings.TrimSpace(lines[1])
			require.Equal(t, clonePath, dataDirectoryLine, "postmaster.pid should contain correct data directory")
		}

		// Fourth line contains the port number
		portLine := strings.TrimSpace(lines[3])
		require.Equal(t, fmt.Sprintf("%d", checkoutResult.Port), portLine, "postmaster.pid should contain correct port")

		// Verify postgresql.auto.conf is configured for clone
		autoConfPath := clonePath + "/postgresql.auto.conf"
		cmd = exec.Command("sudo", "test", "-f", autoConfPath)
		require.NoError(t, cmd.Run(), "postgresql.auto.conf should exist")

		cmd = exec.Command("sudo", "cat", autoConfPath)
		output, err = cmd.Output()
		require.NoError(t, err, "Should be able to read postgresql.auto.conf")
		autoConfContent := string(output)
		require.Contains(t, autoConfContent, "# Clone instance", "postgresql.auto.conf should contain clone identifier")
		require.Contains(t, autoConfContent, "archive_mode = 'off'", "postgresql.auto.conf should disable archiving")
		require.Contains(t, autoConfContent, "restore_command = ''", "postgresql.auto.conf should clear restore command")

		// Verify postgresql.conf is configured with clone-specific settings
		postgresqlConfPath := clonePath + "/postgresql.conf"
		cmd = exec.Command("sudo", "cat", postgresqlConfPath)
		output, err = cmd.Output()
		require.NoError(t, err, "Should be able to read postgresql.conf")
		postgresqlConfContent := string(output)
		require.Contains(t, postgresqlConfContent, "listen_addresses = '*'", "postgresql.conf should allow external connections")
		require.Contains(t, postgresqlConfContent, "max_connections = 5", "postgresql.conf should have clone-optimized connection limit")
		require.Contains(t, postgresqlConfContent, "wal_level = minimal", "postgresql.conf should use minimal WAL level")
		require.Contains(t, postgresqlConfContent, "synchronous_commit = off", "postgresql.conf should disable synchronous commit")

		// Verify pg_hba.conf allows admin user access
		pgHbaConfPath := clonePath + "/pg_hba.conf"
		cmd = exec.Command("sudo", "cat", pgHbaConfPath)
		output, err = cmd.Output()
		require.NoError(t, err, "Should be able to read pg_hba.conf")
		pgHbaConfContent := string(output)
		require.Contains(t, pgHbaConfContent, "host    all             admin           0.0.0.0/0               md5", "pg_hba.conf should allow admin user from any IP")

		// Verify WAL directory exists and has proper permissions
		walDirPath := clonePath + "/pg_wal"
		cmd = exec.Command("sudo", "test", "-d", walDirPath)
		require.NoError(t, cmd.Run(), "pg_wal directory should exist")

		// Verify WAL directory permissions (should be accessible only by postgres user for security)
		cmd = exec.Command("sudo", "stat", "-c", "%a", walDirPath)
		output, err = cmd.Output()
		require.NoError(t, err, "Should be able to check WAL directory permissions")
		permissions := strings.TrimSpace(string(output))
		require.Equal(t, "700", permissions, "pg_wal directory should have 700 permissions (owner only for security)")
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
