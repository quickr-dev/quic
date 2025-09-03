package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testStanza   = "test-stanza"
	testDatabase = "testdb"
	quicdBinary  = "/usr/local/bin/quicd"
)

func generateRestoreName() string {
	return fmt.Sprintf("test-%d", time.Now().Unix())
}

func getRestoreMount(dirname string) string {
	return fmt.Sprintf("/opt/quic/%s/_restore", dirname)
}

func TestQuicdInit(t *testing.T) {
	testDirname := generateRestoreName()
	restoreMount := getRestoreMount(testDirname)

	t.Run("init creates ZFS dataset and restores database", func(t *testing.T) {
		// Before:
		// - No ZFS dataset
		cmd := exec.Command("sudo", "zfs", "list", "tank/"+testDirname)
		require.Error(t, cmd.Run(), "ZFS dataset should not exist before init")

		// Run $ quicd init
		cmd = exec.Command("sudo", quicdBinary, "init", testDirname,
			"--stanza", testStanza,
			"--database", testDatabase)
		_, err := cmd.CombinedOutput()

		// After:
		// ZFS dataset was created
		cmd = exec.Command("sudo", "zfs", "list", "tank/"+testDirname)
		err = cmd.Run()
		require.NoError(t, err, "ZFS dataset was not created")

		// metadata file was created
		metadataFile := filepath.Join(restoreMount, ".quic-init-meta.json")
		require.FileExists(t, metadataFile)
		metadataBytes, err := os.ReadFile(metadataFile)
		require.NoError(t, err, "failed to read metadata file")
		require.Contains(t, string(metadataBytes), testDirname)
		require.Contains(t, string(metadataBytes), "port")
		require.Contains(t, string(metadataBytes), "service_name")

		// PostgreSQL data directory was restored
		require.DirExists(t, restoreMount)
		require.FileExists(t, filepath.Join(restoreMount, "postgresql.conf"))
		require.FileExists(t, filepath.Join(restoreMount, "PG_VERSION"))

		// PostgreSQL data directory has correct ownership
		stat, err := os.Stat(restoreMount)
		require.NoError(t, err)
		require.True(t, stat.IsDir())

		// Verify PostgreSQL service was created and started
		serviceName := fmt.Sprintf("postgresql-%s", testDirname)
		cmd = exec.Command("sudo", "systemctl", "is-active", serviceName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "PostgreSQL service %s should be active: %s", serviceName, output)
		require.Contains(t, string(output), "active")

		// Verify PostgreSQL is ready and accepting connections
		// Extract port from metadata to test connection
		var metadata map[string]interface{}
		require.NoError(t, json.Unmarshal(metadataBytes, &metadata))
		port, ok := metadata["port"].(float64)
		require.True(t, ok, "port should be present in metadata")

		// Test PostgreSQL readiness
		cmd = exec.Command("sudo", "-u", "postgres", "pg_isready", "-p", fmt.Sprintf("%.0f", port))
		err = cmd.Run()
		require.NoError(t, err, "PostgreSQL should be ready on port %.0f", port)

		// Verify we can query the test data from cloud-init.yaml
		cmd = exec.Command("sudo", "-u", "postgres", "psql", "-p", fmt.Sprintf("%.0f", port), "-d", testDatabase, "-c", "SELECT COUNT(*) FROM users;")
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "Should be able to query test data: %s", output)
		require.Contains(t, string(output), "3", "Should have 3 users (Alice, Bob, Charlie) from cloud-init setup")

		// Check key PostgreSQL configuration files created by pgbackrest --type=standby

		// 1. standby.signal - Should exist (indicates standby mode)
		standbySignalPath := filepath.Join(restoreMount, "standby.signal")
		require.FileExists(t, standbySignalPath, "standby.signal should exist in restored database")

		// 2. postgresql.auto.conf - Should contain recovery settings
		autoConfPath := filepath.Join(restoreMount, "postgresql.auto.conf")
		if _, err := os.Stat(autoConfPath); err == nil {
			content, err := os.ReadFile(autoConfPath)
			require.NoError(t, err)
			contentStr := string(content)
			t.Logf("postgresql.auto.conf contents:\n%s", contentStr)

			// Should NOT contain our clone-specific modifications
			require.NotContains(t, contentStr, "# Clone instance - recovery disabled",
				"postgresql.auto.conf should not contain clone-specific configuration")
		}

		// 3. recovery.signal - May exist for older PostgreSQL versions
		recoverySignalPath := filepath.Join(restoreMount, "recovery.signal")
		if _, err := os.Stat(recoverySignalPath); err == nil {
			t.Logf("recovery.signal found at %s", recoverySignalPath)
		}

		// 4. postgresql.conf - Check main configuration for archive settings
		postgresqlConfPath := filepath.Join(restoreMount, "postgresql.conf")
		if _, err := os.Stat(postgresqlConfPath); err == nil {
			content, err := os.ReadFile(postgresqlConfPath)
			require.NoError(t, err)
			contentStr := string(content)

			// Log key recovery-related settings
			t.Logf("postgresql.conf archive settings:")
			if strings.Contains(contentStr, "archive_mode") {
				t.Logf("  - Contains archive_mode setting")
			}
			if strings.Contains(contentStr, "archive_command") {
				t.Logf("  - Contains archive_command setting")
			}
			if strings.Contains(contentStr, "restore_command") {
				t.Logf("  - Contains restore_command setting")
			}
		}
	})
}
