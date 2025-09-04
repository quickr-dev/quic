package e2e_agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os/exec"
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
	cmd := exec.Command("multipass", "info", "quic-e2e", "--format", "json")
	output, err := cmd.Output()
	require.NoError(t, err, "Failed to get VM info")

	var info MultipassInfo
	err = json.Unmarshal(output, &info)
	require.NoError(t, err, "Failed to parse VM info JSON")

	vmInfo, exists := info.Info["quic-e2e"]
	require.True(t, exists, "VM quic-e2e not found")
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

// VM command execution helpers
func execInVM(t *testing.T, cmd ...string) (string, error) {
	args := append([]string{"exec", "quic-e2e", "--"}, cmd...)
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

// Directory assertion helpers (execute in VM)
func assertDirExists(t *testing.T, dirPath string) {
	_, err := execInVMSudo(t, "test", "-d", dirPath)
	assert.NoError(t, err, "Directory %s should exist in VM", dirPath)
}

func assertDirNotExists(t *testing.T, dirPath string) {
	_, err := execInVMSudo(t, "test", "-d", dirPath)
	assert.Error(t, err, "Directory %s should NOT exist in VM", dirPath)
}
