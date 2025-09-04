package e2e

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/quickr-dev/quic/proto"
)

const (
	testStanza   = "test-stanza"
	testDatabase = "testdb"
)

// MultipassInfo represents the JSON structure returned by multipass info command
type MultipassInfo struct {
	Info map[string]struct {
		IPv4 []string `json:"ipv4"`
	} `json:"info"`
}

// getVMIP retrieves the IP address of the test VM
func getVMIP(t *testing.T) string {
	cmd := exec.Command("multipass", "info", "quic-e2e-base", "--format", "json")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to get VM info")

	var info MultipassInfo
	err = json.Unmarshal(output, &info)
	require.NoError(t, err, "Failed to parse VM info JSON")

	vmInfo, exists := info.Info["quic-e2e-base"]
	require.True(t, exists, "VM quic-e2e-base not found")
	require.NotEmpty(t, vmInfo.IPv4, "VM has no IPv4 address")

	return vmInfo.IPv4[0]
}

// setupGRPCClient creates a gRPC client connection to the test VM
func setupGRPCClient(t *testing.T) (pb.QuicServiceClient, context.Context, context.CancelFunc) {
	vmIP := getVMIP(t)
	conn, err := grpc.Dial(vmIP+":8443", grpc.WithTransportCredentials(
		credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}),
	))
	require.NoError(t, err, "Failed to connect to gRPC server")
	t.Cleanup(func() { conn.Close() })

	client := pb.NewQuicServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	return client, ctx, cancel
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// VM command execution helpers
func execInVM(t *testing.T, cmd ...string) (string, error) {
	args := append([]string{"exec", "quic-e2e-base", "--"}, cmd...)
	multipassCmd := exec.Command("multipass", args...)
	output, err := multipassCmd.Output()
	return strings.TrimSpace(string(output)), err
}

func execInVMSudo(t *testing.T, cmd ...string) (string, error) {
	sudoCmd := append([]string{"sudo"}, cmd...)
	return execInVM(t, sudoCmd...)
}

func assertExecInVMSuccess(t *testing.T, cmd ...string) string {
	output, err := execInVM(t, cmd...)
	require.NoError(t, err, "Command should succeed in VM: %v", cmd)
	return output
}

func assertExecInVMSudoSuccess(t *testing.T, cmd ...string) string {
	output, err := execInVMSudo(t, cmd...)
	require.NoError(t, err, "Sudo command should succeed in VM: %v", cmd)
	return output
}

// ZFS helper functions (execute in VM)
func datasetExists(dataset string) bool {
	_, err := execInVMSudo(&testing.T{}, "zfs", "list", "-H", "-o", "name", dataset)
	return err == nil
}

func snapshotExists(snapshot string) bool {
	_, err := execInVMSudo(&testing.T{}, "zfs", "list", "-H", "-o", "name", snapshot)
	return err == nil
}

func datasetExistsInVM(t *testing.T, dataset string) bool {
	_, err := execInVMSudo(t, "zfs", "list", "-H", "-o", "name", dataset)
	return err == nil
}

func snapshotExistsInVM(t *testing.T, snapshot string) bool {
	_, err := execInVMSudo(t, "zfs", "list", "-H", "-o", "name", snapshot)
	return err == nil
}

// File assertion helpers (execute in VM)
func assertFileExists(t *testing.T, filePath string) bool {
	_, err := execInVMSudo(t, "test", "-f", filePath)
	if err != nil {
		t.Logf("File %s does not exist in VM", filePath)
		return false
	}
	return true
}

func assertFileNotExists(t *testing.T, filePath string) {
	_, err := execInVMSudo(t, "test", "-f", filePath)
	assert.Error(t, err, "File %s should NOT exist in VM", filePath)
}

func assertFileContains(t *testing.T, filePath, expectedContent string) {
	output, err := execInVMSudo(t, "cat", filePath)
	require.NoError(t, err, "Failed to read file %s in VM", filePath)

	assert.Contains(t, output, expectedContent,
		"File %s should contain '%s'", filePath, expectedContent)
}

func assertFileDoesNotContain(t *testing.T, filePath, unwantedContent string) {
	output, err := execInVMSudo(t, "cat", filePath)
	require.NoError(t, err, "Failed to read file %s in VM", filePath)

	assert.NotContains(t, output, unwantedContent,
		"File %s should not contain '%s'", filePath, unwantedContent)
}

// Process and port helpers (execute in VM)
func assertPostgresProcessRunning(t *testing.T, clonePath string) {
	_, err := execInVM(t, "pgrep", "-f", clonePath)
	assert.NoError(t, err, "PostgreSQL process should be running for clone at %s in VM", clonePath)
}

func assertPostgresProcessNotRunning(t *testing.T, clonePath string) {
	_, err := execInVM(t, "pgrep", "-f", clonePath)
	assert.Error(t, err, "PostgreSQL process should NOT be running for clone at %s in VM", clonePath)
}

func assertPortInUse(t *testing.T, port int) {
	output, err := execInVM(t, "ss", "-tlnp")
	require.NoError(t, err, "Failed to check port status in VM")

	portStr := ":" + fmt.Sprintf("%d", port)
	assert.Contains(t, output, portStr, "Port %d should be in use in VM", port)
}

func assertPortNotInUse(t *testing.T, port int) {
	output, err := execInVM(t, "ss", "-tlnp")
	require.NoError(t, err, "Failed to check port status in VM")

	portStr := ":" + fmt.Sprintf("%d", port)
	assert.NotContains(t, output, portStr, "Port %d should NOT be in use in VM", port)
}

// System helpers (execute in VM)
func assertPostgresOwnership(t *testing.T, path string) {
	output, err := execInVM(t, "stat", "-c", "%U:%G", path)
	require.NoError(t, err, "Failed to check ownership in VM")

	assert.Equal(t, "postgres:postgres", output,
		"Directory %s should be owned by postgres:postgres in VM", path)
}

func getUFWStatus(t *testing.T) string {
	output, err := execInVMSudo(t, "ufw", "status")
	require.NoError(t, err, "Failed to get UFW status in VM")
	return output
}

// Database connection helpers
func assertAdminUserCanConnect(t *testing.T, port int, adminPassword string) {
	// Build connection string using VM IP
	vmIP := getVMIP(t)
	connStr := fmt.Sprintf("postgres://admin:%s@%s:%d/postgres?sslmode=disable",
		adminPassword, vmIP, port)

	// Retry connection a few times as PostgreSQL might take a moment to be ready
	var db *sql.DB
	var err error

	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			err = db.Ping()
			if err == nil {
				break
			}
			db.Close()
		}
		time.Sleep(1 * time.Second)
	}

	require.NoError(t, err, "Admin user should be able to connect to PostgreSQL on port %d", port)
	require.NotNil(t, db, "Database connection should not be nil")

	// Test that admin user has superuser privileges by creating a test table
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS test_admin_privileges (id serial primary key)")
	assert.NoError(t, err, "Admin user should have CREATE TABLE privileges")

	// Clean up
	_, err = db.Exec("DROP TABLE IF EXISTS test_admin_privileges")
	assert.NoError(t, err, "Admin user should have DROP TABLE privileges")

	db.Close()
}

// Connection string parsing helpers
func parseConnectionString(connStr string) (port int, adminPassword string, err error) {
	// Format: postgres://admin:PASSWORD@HOST:PORT/postgres?sslmode=disable
	// Using regex to handle URL encoding and special characters in passwords
	parts := strings.Split(connStr, "@")
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid connection string format")
	}

	// Extract user:password from first part
	userPart := strings.TrimPrefix(parts[0], "postgres://admin:")
	adminPassword = userPart

	// Extract host:port from second part
	hostPortPart := strings.Split(parts[1], "/")[0]
	hostPortParts := strings.Split(hostPortPart, ":")
	if len(hostPortParts) != 2 {
		return 0, "", fmt.Errorf("invalid host:port format")
	}

	port, err = strconv.Atoi(hostPortParts[1])
	if err != nil {
		return 0, "", fmt.Errorf("invalid port: %w", err)
	}

	return port, adminPassword, nil
}

// Directory assertion helpers (execute in VM)
func assertDirExists(t *testing.T, dirPath string) {
	_, err := execInVMSudo(t, "test", "-d", dirPath)
	assert.NoError(t, err, "Directory %s should exist in VM", dirPath)
}

func assertDirNotExists(t *testing.T, dirPath string) {
	_, err := execInVMSudo(t, "test", "-d", dirPath)
	assert.Error(t, err, "Directory %s should NOT exist in VM", dirPath)
}
