package e2e_agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/quickr-dev/quic/internal/agent"
)

const (
	createdBy = "username"
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
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
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")

		ufwAfter := getUFWStatus(t)

		// Verify firewall rule was not present before checkout
		portStr := fmt.Sprintf("%d/tcp", checkoutResult.Port)
		require.NotContains(t, ufwBefore, portStr, "UFW should not contain port %d before checkout", checkoutResult.Port)

		// Verify firewall rule was added for the checkout port
		require.Contains(t, ufwAfter, portStr, "UFW should contain port %d after checkout", checkoutResult.Port)
	})

	t.Run("SaveMetadataFile", func(t *testing.T) {
		cloneName := generateCloneName()

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")

		// Verify metadata file exists
		metadataPath := filepath.Join(checkoutResult.ClonePath, ".quic-meta.json")
		require.FileExists(t, metadataPath, "Metadata file should exist")

		// Read and verify metadata content
		metadataBytes, err := os.ReadFile(metadataPath)
		require.NoError(t, err, "Should be able to read metadata file")

		var metadata map[string]interface{}
		require.NoError(t, json.Unmarshal(metadataBytes, &metadata), "Should be able to parse metadata JSON")

		// Verify all expected fields are present
		require.Equal(t, cloneName, metadata["clone_name"], "clone_name should match")
		require.Equal(t, float64(checkoutResult.Port), metadata["port"], "port should match")
		require.Equal(t, checkoutResult.ClonePath, metadata["clone_path"], "clone_path should match")
		require.Equal(t, checkoutResult.AdminPassword, metadata["admin_password"], "admin_password should not be empty")
		require.Equal(t, createdBy, metadata["created_by"], "created_by should match")
		require.Equal(t, checkoutResult.CreatedAt.UTC().Format(time.RFC3339), metadata["created_at"])
		require.Equal(t, checkoutResult.UpdatedAt.UTC().Format(time.RFC3339), metadata["updated_at"])

	})

	t.Run("DuplicateCheckoutReturnsExistingOne", func(t *testing.T) {
		cloneName := generateCloneName()

		// Create first checkout
		checkoutResult1, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "First CreateCheckout should succeed")
		require.NotNil(t, checkoutResult1, "First CreateCheckout should return result")

		// Create second checkout with same name
		checkoutResult2, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "Second CreateCheckout should succeed")
		require.NotNil(t, checkoutResult2, "Second CreateCheckout should return result")

		require.Equal(t, checkoutResult1, checkoutResult2, "Results should be identical")
	})

	t.Run("InvalidCloneName", func(t *testing.T) {
		// Test reserved name "_restore"
		_, err := service.CreateCheckout(context.Background(), "_restore", restoreResult.Dirname, createdBy)
		require.Error(t, err, "Should reject reserved name '_restore'")
		require.Equal(t, "invalid clone name: clone name '_restore' is reserved", err.Error())

		// Test invalid characters
		_, err = service.CreateCheckout(context.Background(), "test@invalid", restoreResult.Dirname, createdBy)
		require.Error(t, err, "Should reject names with invalid characters")
		require.Equal(t, "invalid clone name: clone name must contain only letters, numbers, underscore, and dash", err.Error())

		// Test empty name
		_, err = service.CreateCheckout(context.Background(), "", restoreResult.Dirname, createdBy)
		require.Error(t, err, "Should reject empty name")
		require.Equal(t, "invalid clone name: clone name must be between 1 and 50 characters", err.Error())

		// Test name too long (over 50 characters)
		longName := strings.Repeat("a", 51)
		_, err = service.CreateCheckout(context.Background(), longName, restoreResult.Dirname, createdBy)
		require.Error(t, err, "Should reject names longer than 50 characters")
		require.Equal(t, "invalid clone name: clone name must be between 1 and 50 characters", err.Error())
	})

	t.Run("AuditLogEntry", func(t *testing.T) {
		cloneName := generateCloneName()
		auditLogPath := "/var/log/quic/audit.log"

		// Get initial audit log size (if exists)
		var initialSize int64
		if info, err := os.Stat(auditLogPath); err == nil {
			initialSize = info.Size()
		}

		// Create checkout
		checkoutResult, err := service.CreateCheckout(context.Background(), cloneName, restoreResult.Dirname, createdBy)
		require.NoError(t, err, "CreateCheckout should succeed")

		// Verify audit log was updated
		info, err := os.Stat(auditLogPath)
		require.NoError(t, err, "Audit log file should exist")
		require.Greater(t, info.Size(), initialSize, "Audit log should have grown")

		// Read the last line of the audit log
		cmd := exec.Command("tail", "-n", "1", auditLogPath)
		output, err := cmd.Output()
		require.NoError(t, err, "Should be able to read last line of audit log")
		lastLine := strings.TrimSpace(string(output))
		require.NotEmpty(t, lastLine, "Should have read a log entry")

		// Parse audit log entry
		auditEntry, err := agent.ParseAuditEntry(lastLine)
		require.NoError(t, err, "Should be able to parse audit log entry")

		// Verify audit entry structure
		require.Equal(t, "checkout_create", auditEntry["event_type"], "Event type should be checkout_create")
		require.Contains(t, auditEntry, "timestamp", "Should have timestamp")
		require.Contains(t, auditEntry, "details", "Should have details")

		// Verify details contain checkout information
		details, ok := auditEntry["details"].(map[string]interface{})
		require.True(t, ok, "Details should be an object")
		require.Equal(t, cloneName, details["clone_name"], "Details should contain clone name")
		require.Equal(t, float64(checkoutResult.Port), details["port"], "Details should contain port")
		require.Equal(t, checkoutResult.ClonePath, details["clone_path"], "Details should contain clone path")
		require.Equal(t, createdBy, details["created_by"], "Details should contain created_by")
	})
}
