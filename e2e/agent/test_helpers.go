package e2e_agent

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testStanza   = "test-stanza"
	testDatabase = "testdb"
)

func getRestorePath(dirname string) string {
	return fmt.Sprintf("/opt/quic/%s/_restore", dirname)
}

func generateCloneName() string {
	return fmt.Sprintf("test-clone-%d", time.Now().Unix())
}

func verifyZFSDatasetExists(t *testing.T, datasetName string, shouldExist bool) {
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", datasetName)
	err := cmd.Run()
	if shouldExist {
		require.NoError(t, err, "Dataset %s should exist", datasetName)
	} else {
		require.Error(t, err, "Dataset %s should not exist", datasetName)
	}
}

func verifyZFSMountpoint(t *testing.T, datasetName, expectedMountpoint string) {
	cmd := exec.Command("sudo", "zfs", "get", "-H", "-o", "value", "mountpoint", datasetName)
	output, err := cmd.Output()
	require.NoError(t, err, "Should be able to get mountpoint for %s", datasetName)

	mountpoint := strings.TrimSpace(string(output))
	require.Equal(t, expectedMountpoint, mountpoint, "Dataset %s should have expected mountpoint", datasetName)
}

func verifyFileExists(t *testing.T, filePath string, shouldExist bool) {
	cmd := exec.Command("sudo", "test", "-f", filePath)
	err := cmd.Run()
	if shouldExist {
		require.NoError(t, err, "File %s should exist", filePath)
	} else {
		require.Error(t, err, "File %s should not exist", filePath)
	}
}

func verifyDirectoryExists(t *testing.T, dirPath string) {
	cmd := exec.Command("sudo", "test", "-d", dirPath)
	require.NoError(t, cmd.Run(), "Directory %s should exist", dirPath)
}

func verifyDirectoryPermissions(t *testing.T, dirPath, expectedPermissions string) {
	cmd := exec.Command("sudo", "stat", "-c", "%a", dirPath)
	output, err := cmd.Output()
	require.NoError(t, err, "Should be able to check permissions for %s", dirPath)

	permissions := strings.TrimSpace(string(output))
	require.Equal(t, expectedPermissions, permissions, "Directory %s should have %s permissions", dirPath, expectedPermissions)
}

func readFileContent(t *testing.T, filePath string) string {
	cmd := exec.Command("sudo", "cat", filePath)
	output, err := cmd.Output()
	require.NoError(t, err, "Should be able to read %s", filePath)
	return string(output)
}

func verifyFileContains(t *testing.T, filePath, expectedContent, description string) {
	content := readFileContent(t, filePath)
	require.Contains(t, content, expectedContent, "%s: %s", filePath, description)
}

func parsePostmasterPid(t *testing.T, clonePath string) map[string]interface{} {
	postmasterPidPath := clonePath + "/postmaster.pid"
	verifyFileExists(t, postmasterPidPath, true)

	content := readFileContent(t, postmasterPidPath)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	require.GreaterOrEqual(t, len(lines), 4, "postmaster.pid should have at least 4 lines")

	result := make(map[string]interface{})
	if len(lines) >= 1 {
		result["pid"] = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 {
		result["dataDirectory"] = strings.TrimSpace(lines[1])
	}
	if len(lines) >= 3 {
		result["startTime"] = strings.TrimSpace(lines[2])
	}
	if len(lines) >= 4 {
		portStr := strings.TrimSpace(lines[3])
		port, err := strconv.Atoi(portStr)
		require.NoError(t, err, "port should be a valid integer")
		result["port"] = port
	}

	return result
}

func touch(t *testing.T, filePath string) {
	cmd := exec.Command("sudo", "touch", filePath)
	require.NoError(t, cmd.Run(), "Should be able to create empty file %s", filePath)
}

func assertSystemdServiceRunning(t *testing.T, serviceName string) {
	cmd := exec.Command("sudo", "systemctl", "is-active", serviceName)
	output, err := cmd.Output()
	require.NoError(t, err, "Service %s should be running", serviceName)

	status := strings.TrimSpace(string(output))
	require.Equal(t, "active", status, "Service %s should be active", serviceName)
}

func assertCloneInstanceRunning(t *testing.T, clonePath string) {
	pidData := parsePostmasterPid(t, clonePath)
	pid := pidData["pid"].(string)

	// Verify the PID from postmaster.pid is running
	cmd := exec.Command("sudo", "ps", "-p", pid, "-o", "comm=")
	output, err := cmd.Output()
	require.NoError(t, err, "PostgreSQL process should be running with PID %s", pid)

	processName := strings.TrimSpace(string(output))
	require.Equal(t, "postgres", processName, "Process should be postgres")

	// Cross-verify using pgrep to find PostgreSQL process for this specific clone path
	cmd = exec.Command("pgrep", "-f", clonePath)
	pgrepOutput, err := cmd.Output()
	require.NoError(t, err, "Should find PostgreSQL process for clone path %s", clonePath)

	pgrepPid := strings.TrimSpace(string(pgrepOutput))
	require.Equal(t, pid, pgrepPid, "PID from postmaster.pid should match pgrep result for clone path")
}
