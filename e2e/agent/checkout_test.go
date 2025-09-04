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
		// Test configuring clone (remove standby.signal, update postgresql.auto.conf, etc.)
		t.Skip("Not yet implemented")
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
