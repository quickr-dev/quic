package e2e

import (
	"context"
	"fmt"
	"os/exec"
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
	_ = service
	_ = restoreResult

	t.Run("CreatesZFSSnapshot", func(t *testing.T) {
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
		// Test creating a ZFS clone from snapshot
		t.Skip("Not yet implemented")
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
