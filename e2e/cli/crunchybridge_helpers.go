package e2e_cli

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/quickr-dev/quic/internal/providers"
)

const (
	quicE2eClusterName = "quic-e2e"
)

// Ensures a CrunchyBridge cluster exists with at least one backup available
func ensureCrunchyBridgeBackup(t *testing.T, clusterName string) (*providers.Cluster, []providers.Backup, string, error) {
	t.Helper()

	// Get API key from environment using test config helper
	apiKey := getRequiredTestEnv("CB_API_KEY")
	if apiKey == "" {
		return nil, nil, "", fmt.Errorf("CB_API_KEY environment variable is required")
	}

	client := providers.NewCrunchyBridgeClient(apiKey)

	// Check if cluster already exists
	cluster, err := client.FindClusterByName(clusterName)
	if err != nil {
		// Cluster doesn't exist, create it
		createReq := providers.CreateClusterRequest{
			Name:       clusterName,
			PlanID:     "hobby-0",
			TeamID:     "z3pexxorhbcvlk726kscmxtidi",
			ProviderID: "aws",
			RegionID:   "us-east-1",
		}

		postgresVersion := 16
		storage := 10
		createReq.PostgresVersionID = &postgresVersion
		createReq.Storage = &storage

		t.Logf("Creating CrunchyBridge cluster: %s", clusterName)
		cluster, err = client.CreateCluster(createReq)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to create cluster: %w", err)
		}

		t.Logf("Created cluster %s with ID: %s", cluster.Name, cluster.ID)

		// Wait for cluster to be ready before starting backup
		t.Logf("Waiting for cluster to be ready...")
		maxWait := 10 * time.Minute
		pollInterval := 30 * time.Second
		startTime := time.Now()

		for time.Since(startTime) < maxWait {
			cluster, err = client.GetCluster(cluster.ID)
			if err != nil {
				return nil, nil, "", fmt.Errorf("failed to get cluster state: %w", err)
			}

			t.Logf("Cluster state: %s (elapsed: %s)", cluster.State, time.Since(startTime).Round(time.Second))

			if cluster.State == "ready" {
				t.Logf("Cluster is ready!")
				break
			}

			if cluster.State == "error" || cluster.State == "failed" {
				return nil, nil, "", fmt.Errorf("cluster creation failed with state: %s", cluster.State)
			}

			time.Sleep(pollInterval)
		}

		if cluster.State != "ready" {
			return nil, nil, "", fmt.Errorf("timeout waiting for cluster to be ready after %s, current state: %s", maxWait, cluster.State)
		}
	}

	// Get postgres superuser connection string first
	postgresRole, err := client.GetRole(cluster.ID, "postgres")
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get postgres role: %w", err)
	}

	t.Logf("Retrieved connection string for cluster %s", cluster.Name)

	// Set up test database with data BEFORE creating backup
	err = setupTestDatabase(t, postgresRole.URI)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to setup test database: %w", err)
	}

	// List existing backups
	backups, err := client.ListBackups(cluster.ID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to list backups: %w", err)
	}

	// If no backups exist, start one and wait for it (backup will contain our test data)
	if len(backups) == 0 {
		t.Logf("No backups found for cluster %s, starting backup (will include test data)", cluster.Name)
		err = client.StartBackup(cluster.ID)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to start backup: %w", err)
		}

		// Poll for backup completion (with timeout)
		maxWait := 3 * time.Minute
		pollInterval := 10 * time.Second
		startTime := time.Now()

		for time.Since(startTime) < maxWait {
			t.Logf("Waiting for backup to complete... (%s elapsed)", time.Since(startTime).Round(time.Second))
			time.Sleep(pollInterval)

			backups, err = client.ListBackups(cluster.ID)
			if err != nil {
				return nil, nil, "", fmt.Errorf("failed to poll backups: %w", err)
			}

			if len(backups) > 0 {
				t.Logf("Backup completed! Found %d backup(s)", len(backups))
				break
			}
		}

		if len(backups) == 0 {
			return nil, nil, "", fmt.Errorf("timeout waiting for backup to complete after %s", maxWait)
		}
	} else {
		t.Logf("Found %d existing backup(s) for cluster %s", len(backups), cluster.Name)
	}

	return cluster, backups, postgresRole.URI, nil
}

// setupTestDatabase creates a test database with sample data
func setupTestDatabase(t *testing.T, connectionString string) error {
	t.Helper()

	// Connect to postgres database (default database)
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	t.Logf("Connected to PostgreSQL successfully")

	// Create quic_test database
	_, err = db.Exec("CREATE DATABASE quic_test")
	if err != nil {
		// Database might already exist, try to continue
		t.Logf("Database creation failed (might already exist): %v", err)
	} else {
		t.Logf("Created database 'quic_test'")
	}

	// Close connection to default database and connect to quic_test
	db.Close()

	// Modify connection string to use quic_test database (replace the database name at the end)
	if !strings.HasSuffix(connectionString, "/postgres") {
		return fmt.Errorf("connection string does not end with /postgres: %s", connectionString)
	}
	testConnectionString := connectionString[:len(connectionString)-9] + "/quic_test"

	testDB, err := sql.Open("postgres", testConnectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to quic_test database: %w", err)
	}
	defer testDB.Close()

	// Create users table
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL UNIQUE
		)
	`

	_, err = testDB.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	t.Logf("Created users table")

	// Ensure database has exactly the expected test users
	expectedUsers := []string{"Alice Johnson", "Bob Smith", "Charlie Brown", "Diana Ross", "Eve Wilson"}

	// Check current state
	var existingCount int
	err = testDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&existingCount)
	if err != nil {
		return fmt.Errorf("failed to count existing users: %w", err)
	}

	// Check if we have exactly the expected users
	hasCorrectUsers := existingCount == len(expectedUsers)
	if hasCorrectUsers {
		// Verify all expected users exist
		for _, expectedUser := range expectedUsers {
			var count int
			err = testDB.QueryRow("SELECT COUNT(*) FROM users WHERE name = $1", expectedUser).Scan(&count)
			if err != nil {
				return fmt.Errorf("failed to check user %s: %w", expectedUser, err)
			}
			if count != 1 {
				hasCorrectUsers = false
				break
			}
		}
	}

	if !hasCorrectUsers {
		t.Logf("Database state needs correction. Found %d users, expected %d specific users", existingCount, len(expectedUsers))

		// Clean up existing data
		_, err = testDB.Exec("DELETE FROM users")
		if err != nil {
			return fmt.Errorf("failed to clean existing users: %w", err)
		}

		// Insert the expected users
		for _, name := range expectedUsers {
			_, err = testDB.Exec("INSERT INTO users (name) VALUES ($1)", name)
			if err != nil {
				return fmt.Errorf("failed to insert user %s: %w", name, err)
			}
		}

		t.Logf("Reset database to correct state with %d users", len(expectedUsers))
	} else {
		t.Logf("Database already has correct %d users", existingCount)
	}

	// Verify data was inserted
	var count int
	err = testDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}

	t.Logf("Total users in database: %d", count)

	return nil
}
