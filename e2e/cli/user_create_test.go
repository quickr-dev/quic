package e2e_cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/quickr-dev/quic/internal/db"
	"github.com/stretchr/testify/require"
)

func TestQuicUserCreate(t *testing.T) {
	vmIP := ensureVMRunning(t, QuicUserVM)
	// need to run host setup to start quicd that inits the SQLite db
	runQuic(t, "host", "new", vmIP, "--devices", VMDevices)
	runQuicHostSetupWithAck(t, QuicUserVM)

	t.Run("successful user creation", func(t *testing.T) {
		output, err := runQuic(t, "user", "create", "John Doe")
		require.NoError(t, err, output)

		require.Contains(t, output, "created successfully")
		require.Contains(t, output, "To use this token, run:")
		require.Contains(t, output, "$ quic login --token")

		token := extractTokenFromOutput(t, output)
		require.NotEmpty(t, token)

		users := queryVMDatabase(t, vmIP)
		require.GreaterOrEqual(t, len(users), 1, "Expected at least one user in database")

		var foundUser map[string]string
		for _, user := range users {
			if user["name"] == "John Doe" {
				foundUser = user
				break
			}
		}
		require.NotNil(t, foundUser, "Should find 'John Doe' in database")
		require.Equal(t, token, foundUser["token"], "Token should match")
		require.NotEmpty(t, foundUser["created_at"], "Created timestamp should exist")
	})

	t.Run("idempotent user creation", func(t *testing.T) {
		// Create user first time
		output1, err := runQuic(t, "user", "create", "Test User")
		require.NoError(t, err, "First user creation should succeed\nOutput: %s", output1)

		token1 := extractTokenFromOutput(t, output1)

		// Create same user again (should update)
		output2, err := runQuic(t, "user", "create", "Test User")
		require.NoError(t, err, "Second user creation should succeed\nOutput: %s", output2)

		token2 := extractTokenFromOutput(t, output2)

		// Verify token was updated
		users := queryVMDatabase(t, vmIP)
		var foundUser map[string]string
		for _, user := range users {
			if user["name"] == "Test User" {
				foundUser = user
				break
			}
		}
		require.NotNil(t, foundUser, "Should find 'Test User' in database")
		require.Equal(t, token2, foundUser["token"], "Token should be updated to new one")
		require.NotEqual(t, token1, token2, "Tokens should be different")
	})
}

func TestQuicUserCreateValidations(t *testing.T) {
	t.Run("user create requires name argument", func(t *testing.T) {
		output, err := runQuic(t, "user", "create")

		require.Error(t, err, "Expected command to fail without name argument")
		require.Contains(t, output, "accepts 1 arg(s), received 0", "Expected argument requirement message in output")
	})

	t.Run("user create requires configured host", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, _ := runQuic(t, "user", "create", "Test User")
		require.Contains(t, output, "no hosts configured", "Expected no hosts message when no hosts configured")
	})
}

func extractTokenFromOutput(t *testing.T, output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "$ quic login --token") {
			parts := strings.Fields(line)
			require.GreaterOrEqual(t, len(parts), 4, "Token line should have at least 4 parts")
			return parts[len(parts)-1] // Last part should be the token
		}
	}
	t.Fatal("Could not find token line in output")
	return ""
}

func queryVMDatabase(t *testing.T, vmIP string) []map[string]string {
	// Query users from VM's SQLite database using the existing runInVM helper
	query := fmt.Sprintf(`sudo sqlite3 %s "SELECT name, token, created_at FROM users;" | while IFS='|' read name token created_at; do echo "{\"name\":\"$name\",\"token\":\"$token\",\"created_at\":\"$created_at\"}"; done`, db.DBPath)

	output := runInVM(t, QuicUserVM, query)

	// Parse JSON lines
	var users []map[string]string
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var user map[string]string
		err := json.Unmarshal([]byte(line), &user)
		require.NoError(t, err, "Failed to parse user JSON: %s", line)
		users = append(users, user)
	}

	return users
}
