package e2e_cli

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/quickr-dev/quic/internal/providers"
	"github.com/stretchr/testify/require"
)

const (
	quicE2eClusterName = "quic-e2e"
)

// Ensures a CrunchyBridge cluster exists with at least one backup available
func ensureCrunchyBridgeBackup(t *testing.T, clusterName string) (*providers.Cluster, []providers.Backup, string, error) {
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

	// Get postgres superuser connection string
	postgresRole, err := client.GetRole(cluster.ID, "postgres")
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get postgres role: %w", err)
	}

	// Set up test database with data BEFORE creating backup
	err = ensureTestDatabase(t, postgresRole.URI)
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

func ensureTestDatabase(t *testing.T, connectionString string) error {
	t.Logf("<recreateTestDatabase>")

	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Check if quic_test database already exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = 'quic_test')").Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if quic_test database exists: %w", err)
	}

	if exists {
		t.Logf("quic_test already exists")
		return nil
	}

	// Create database only if it doesn't exist
	_, err = db.Exec("CREATE DATABASE quic_test")
	if err != nil {
		return fmt.Errorf("failed to create quic_test database: %w", err)
	}

	// Switch to quic_test database and create table with data in one transaction
	if !strings.HasSuffix(connectionString, "/postgres") {
		return fmt.Errorf("connection string does not end with /postgres: %s", connectionString)
	}
	testConnectionString := connectionString[:len(connectionString)-9] + "/quic_test"

	testDB, err := sql.Open("postgres", testConnectionString)
	if err != nil {
		return fmt.Errorf("failed to connect to quic_test database: %w", err)
	}
	defer testDB.Close()

	// Create table and insert test data
	expectedUsers := []string{"Alice Johnson", "Bob Smith", "Charlie Brown", "Diana Ross", "Eve Wilson"}

	_, err = testDB.Exec(`
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL UNIQUE
		)`)
	if err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}

	for _, name := range expectedUsers {
		_, err = testDB.Exec("INSERT INTO users (name) VALUES ($1)", name)
		if err != nil {
			return fmt.Errorf("failed to insert user %s: %w", name, err)
		}
	}

	// Verify data was inserted
	rows, err := testDB.Query("SELECT name FROM users ORDER BY id")
	if err != nil {
		return fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var userNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("failed to scan user name: %w", err)
		}
		userNames = append(userNames, name)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating user rows: %w", err)
	}

	require.Equal(t, expectedUsers, userNames)

	t.Logf("</recreateTestDatabase>")
	return nil
}
