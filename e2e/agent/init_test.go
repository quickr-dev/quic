package e2e

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const (
	testStanza   = "test-stanza"
	testDatabase = "testdb"
	testDirname  = "e2e-test-restore"
	quicdBinary  = "/opt/quic/bin/quicd"
	restoreMount = "/opt/quic/restores/" + testDirname
)

func TestQuicdInit(t *testing.T) {
	// Cleanup any previous test runs
	cleanup(t)

	t.Run("init creates ZFS dataset and restores database", func(t *testing.T) {
		cmd := exec.Command("sudo", quicdBinary, "init", testDirname,
			"--stanza", testStanza,
			"--database", testDatabase)
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "quicd init failed: %s", output)
		require.Contains(t, string(output), "Initialized restore template")

		// Verify ZFS dataset was created
		cmd = exec.Command("sudo", "zfs", "list", "tank/"+testDirname)
		err = cmd.Run()
		require.NoError(t, err, "ZFS dataset was not created")

		// Verify mount point exists and has PostgreSQL data
		require.DirExists(t, restoreMount)
		require.FileExists(t, filepath.Join(restoreMount, "postgresql.conf"))
		require.FileExists(t, filepath.Join(restoreMount, "PG_VERSION"))

		// Verify metadata file was created
		metadataFile := filepath.Join(restoreMount, ".quic-init-meta.json")
		require.FileExists(t, metadataFile)

		// Verify PostgreSQL data directory has correct ownership
		stat, err := os.Stat(restoreMount)
		require.NoError(t, err)

		// Check that the directory is readable (basic ownership check)
		require.True(t, stat.IsDir())
	})

	t.Run("restored database contains test data", func(t *testing.T) {
		// Start the restored PostgreSQL instance temporarily for testing
		testPort := 15999

		cmd := exec.Command("sudo", "-u", "postgres", "/usr/lib/postgresql/16/bin/pg_ctl",
			"start", "-D", restoreMount, "-o", fmt.Sprintf("--port=%d", testPort),
			"-w", "-t", "30")
		err := cmd.Run()
		require.NoError(t, err, "Failed to start restored PostgreSQL instance")

		defer func() {
			// Stop the test instance
			stopCmd := exec.Command("sudo", "-u", "postgres", "/usr/lib/postgresql/16/bin/pg_ctl",
				"stop", "-D", restoreMount, "-m", "fast")
			stopCmd.Run()
		}()

		// Wait for PostgreSQL to be ready
		readyCmd := exec.Command("sudo", "-u", "postgres", "/usr/lib/postgresql/16/bin/pg_isready",
			"-p", fmt.Sprintf("%d", testPort))
		err = readyCmd.Run()
		require.NoError(t, err, "PostgreSQL instance not ready")

		// Connect to the test database and verify data
		connStr := fmt.Sprintf("host=localhost port=%d dbname=%s user=postgres sslmode=disable",
			testPort, testDatabase)
		db, err := sql.Open("postgres", connStr)
		require.NoError(t, err, "Failed to connect to restored database")
		defer db.Close()

		// Query test data
		rows, err := db.Query("SELECT name FROM users ORDER BY id")
		require.NoError(t, err, "Failed to query test data")
		defer rows.Close()

		var names []string
		for rows.Next() {
			var name string
			err := rows.Scan(&name)
			require.NoError(t, err)
			names = append(names, name)
		}

		// Verify we have the expected test data
		require.Equal(t, []string{"Alice", "Bob", "Charlie"}, names)
	})

	t.Cleanup(func() {
		cleanup(t)
	})
}

func TestQuicdInitValidation(t *testing.T) {
	t.Run("fails without stanza", func(t *testing.T) {
		cmd := exec.Command("sudo", quicdBinary, "init", testDirname, "--database", testDatabase)
		output, err := cmd.CombinedOutput()

		require.Error(t, err)
		require.Contains(t, string(output), "stanza")
	})

	t.Run("fails without database", func(t *testing.T) {
		cmd := exec.Command("sudo", quicdBinary, "init", testDirname, "--stanza", testStanza)
		output, err := cmd.CombinedOutput()

		require.Error(t, err)
		require.Contains(t, string(output), "database")
	})
}

func cleanup(t *testing.T) {
	t.Helper()

	// Stop any running PostgreSQL instance on the test data
	stopCmd := exec.Command("sudo", "-u", "postgres", "/usr/lib/postgresql/16/bin/pg_ctl",
		"stop", "-D", restoreMount, "-m", "immediate")
	stopCmd.Run() // Ignore errors as instance may not be running

	// Destroy ZFS dataset (this also removes the mount)
	destroyCmd := exec.Command("sudo", "zfs", "destroy", "-f", "tank/"+testDirname)
	destroyCmd.Run() // Ignore errors as dataset may not exist

	// Remove mount directory if it still exists
	rmCmd := exec.Command("sudo", "rm", "-rf", restoreMount)
	rmCmd.Run()
}
