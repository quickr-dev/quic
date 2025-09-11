package e2e_cli

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestEnsureCrunchyBridgeBackup tests the complete workflow:
// 1. Create "quic-e2e" cluster if it doesn't exist
// 2. Create quic_test database if it doesn't exist
// 3. Create test data if it doesn't exist
// 4. Create backup if it doesn't exist
func TestEnsureCrunchyBridgeBackup(t *testing.T) {
	// Check for required API key using centralized config helper
	apiKey := getRequiredTestEnv("CB_API_KEY")
	require.NotEmpty(t, apiKey, "CB_API_KEY is required (from .env file or environment)")

	clusterName := quicE2eClusterName

	// Call the function under test
	cluster, backups, connectionString, err := ensureCrunchyBridgeBackup(t, clusterName)
	require.NoError(t, err, "ensureCrunchyBridgeBackup should succeed")

	// Verify cluster was created/found
	require.NotNil(t, cluster, "cluster should not be nil")
	require.Equal(t, clusterName, cluster.Name, "cluster name should match")
	require.NotEmpty(t, cluster.ID, "cluster ID should not be empty")
	require.Equal(t, "ready", cluster.State, "cluster should be ready")
	require.NotEmpty(t, cluster.Host, "cluster host should not be empty")

	t.Logf("✓ Cluster verified: %s (ID: %s, State: %s)", cluster.Name, cluster.ID, cluster.State)

	// Verify backups exist
	require.NotEmpty(t, backups, "at least one backup should exist")
	require.Greater(t, len(backups), 0, "backup count should be greater than 0")

	for i, backup := range backups {
		require.NotEmpty(t, backup.Name, "backup %d name should not be empty", i)
		require.NotEmpty(t, backup.Type, "backup %d type should not be empty", i)
		t.Logf("✓ Backup %d: %s (Type: %s, Started: %s)", i+1, backup.Name, backup.Type, backup.StartedAt.Format("2006-01-02 15:04:05"))
	}

	// Verify connection string is valid
	require.NotEmpty(t, connectionString, "connection string should not be empty")
	require.True(t, strings.HasPrefix(connectionString, "postgres://"), "connection string should start with postgres://")
	require.Contains(t, connectionString, cluster.Host, "connection string should contain cluster host")

	t.Logf("✓ Connection string: %s", connectionString)

	// Test database connection and verify test data exists
	testDatabaseContent(t, connectionString)
}

// TestCrunchyBridgeDatabaseState ensures the database is in the expected state
// with exactly 5 unique test users (this test validates the helper function)
func TestCrunchyBridgeDatabaseState(t *testing.T) {
	// Check for required API key
	apiKey := getRequiredTestEnv("CB_API_KEY")
	require.NotEmpty(t, apiKey, "CB_API_KEY is required (from .env file or environment)")

	clusterName := quicE2eClusterName

	// Ensure cluster exists and database is in correct state
	_, _, connectionString, err := ensureCrunchyBridgeBackup(t, clusterName)
	require.NoError(t, err, "should ensure cluster and backup exist")

	t.Logf("Validating database state")

	// Connect to quic_test database and validate final state
	testConnectionString := getQuicTestConnectionString(t, connectionString)

	db, err := sql.Open("postgres", testConnectionString)
	require.NoError(t, err, "should connect to quic_test database")
	defer db.Close()

	// Test connection
	err = db.Ping()
	require.NoError(t, err, "should ping database successfully")

	// Verify final state: exactly 5 users
	var finalCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&finalCount)
	require.NoError(t, err, "should count users")
	require.Equal(t, 5, finalCount, "should have exactly 5 users")

	expectedUsers := []string{"Alice Johnson", "Bob Smith", "Charlie Brown", "Diana Ross", "Eve Wilson"}

	// Verify all expected users exist and are unique
	for _, expectedUser := range expectedUsers {
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM users WHERE name = $1", expectedUser).Scan(&count)
		require.NoError(t, err, "should check count for user %s", expectedUser)
		require.Equal(t, 1, count, "user %s should appear exactly once", expectedUser)
	}

	// List all users for verification
	rows, err := db.Query("SELECT id, name FROM users ORDER BY name")
	require.NoError(t, err, "should query all users")
	defer rows.Close()

	t.Logf("✓ Database state verified - exactly 5 unique users:")
	userNum := 1
	for rows.Next() {
		var id int
		var name string
		err = rows.Scan(&id, &name)
		require.NoError(t, err, "should scan user row")
		t.Logf("  %d. %s (ID: %d)", userNum, name, id)
		userNum++
	}

	require.NoError(t, rows.Err(), "should not have row iteration errors")
}

// getQuicTestConnectionString converts a postgres connection string to quic_test database
func getQuicTestConnectionString(t *testing.T, connectionString string) string {
	t.Helper()

	if !strings.HasSuffix(connectionString, "/postgres") {
		t.Fatalf("connection string does not end with /postgres: %s", connectionString)
	}

	return connectionString[:len(connectionString)-9] + "/quic_test"
}

// testDatabaseContent verifies the test database and data were created correctly
func testDatabaseContent(t *testing.T, connectionString string) {
	t.Helper()

	// Connect to the quic_test database (replace the database name at the end)
	if !strings.HasSuffix(connectionString, "/postgres") {
		t.Fatalf("connection string does not end with /postgres: %s", connectionString)
	}
	testConnectionString := connectionString[:len(connectionString)-9] + "/quic_test"

	db, err := sql.Open("postgres", testConnectionString)
	require.NoError(t, err, "should connect to quic_test database")
	defer db.Close()

	// Test connection
	err = db.Ping()
	require.NoError(t, err, "should ping quic_test database successfully")

	t.Logf("✓ Connected to quic_test database")

	// Verify users table exists
	var tableExists bool
	err = db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_name = 'users'
		)
	`).Scan(&tableExists)
	require.NoError(t, err, "should check if users table exists")
	require.True(t, tableExists, "users table should exist")

	t.Logf("✓ Users table exists")

	// Verify test data exists
	var userCount int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	require.NoError(t, err, "should count users")
	require.Greater(t, userCount, 0, "should have at least some users")

	t.Logf("✓ Found %d users in database", userCount)

	// Verify specific test users exist
	expectedUsers := []string{"Alice Johnson", "Bob Smith", "Charlie Brown", "Diana Ross", "Eve Wilson"}

	for _, expectedUser := range expectedUsers {
		var exists bool
		err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE name = $1)", expectedUser).Scan(&exists)
		require.NoError(t, err, "should check if user %s exists", expectedUser)
		require.True(t, exists, "user %s should exist in database", expectedUser)
	}

	t.Logf("✓ All %d expected test users found", len(expectedUsers))

	// List all users for verification
	rows, err := db.Query("SELECT id, name FROM users ORDER BY name")
	require.NoError(t, err, "should query all users")
	defer rows.Close()

	t.Logf("✓ Database content verified:")
	userNum := 1
	for rows.Next() {
		var id int
		var name string
		err = rows.Scan(&id, &name)
		require.NoError(t, err, "should scan user row")
		t.Logf("  %d. %s (ID: %d)", userNum, name, id)
		userNum++
	}

	require.NoError(t, rows.Err(), "should not have row iteration errors")
}
