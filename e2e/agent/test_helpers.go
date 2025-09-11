package e2e_agent

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/quickr-dev/quic/internal/agent"
)

const (
	testStanza   = "test-stanza"
	testDatabase = "testdb"
	createdBy    = "username"
)

func createRestore(t *testing.T) (*agent.AgentService, *agent.InitResult) {
	// // Create a unique dirname for this restore
	// testDirname := fmt.Sprintf("test-restore-%d", time.Now().UnixNano())

	// // Create checkout service
	// service := agent.NewCheckoutService()

	// // Perform init operation to create restore dataset
	// initConfig := &agent.InitConfig{
	// 	Stanza:   testStanza,
	// 	Database: testDatabase,
	// 	Dirname:  testDirname,
	// }

	// result, err := service.InitRestore(initConfig)
	// require.NoError(t, err, "Restore init should succeed")
	// require.NotNil(t, result)

	// return service, result
	return nil, nil
}

func getTemplatePath(dirname string) string {
	return fmt.Sprintf("/opt/quic/%s/_restore", dirname)
}

func generateCloneName() string {
	return fmt.Sprintf("test-clone-%d", time.Now().UnixNano())
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
	cmd := agent.GetMountpoint(datasetName)
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

func assertSystemdServiceRunning(t *testing.T, serviceName string, shouldBeRunning bool) {
	cmd := exec.Command("sudo", "systemctl", "is-active", serviceName)
	output, _ := cmd.Output() // We don't check error here as it can return non-zero for inactive services

	status := strings.TrimSpace(string(output))
	if shouldBeRunning {
		require.Equal(t, "active", status, "Service %s should be active", serviceName)
	} else {
		require.NotEqual(t, "active", status, "Service %s should not be active", serviceName)
	}
}

func verifySystemdFileExists(t *testing.T, serviceName string, shouldExist bool) {
	serviceFilePath := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	cmd := exec.Command("sudo", "test", "-f", serviceFilePath)
	err := cmd.Run()
	if shouldExist {
		require.NoError(t, err, "Systemd service file %s should exist", serviceFilePath)
	} else {
		require.Error(t, err, "Systemd service file %s should not exist", serviceFilePath)
	}
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

func getUFWStatus(t *testing.T) string {
	cmd := exec.Command("sudo", "ufw", "status")
	output, err := cmd.Output()
	require.NoError(t, err, "Should be able to get UFW status")
	return string(output)
}

func assertUFWTcp(t *testing.T, port int, shouldExist bool, ufwStatus ...string) {
	var status string
	if len(ufwStatus) > 0 {
		status = ufwStatus[0]
	} else {
		status = getUFWStatus(t)
	}

	portStr := fmt.Sprintf("%d/tcp", port)
	if shouldExist {
		require.Contains(t, status, portStr, "UFW should contain rule for port %d", port)
	} else {
		require.NotContains(t, status, portStr, "UFW should not contain rule for port %d", port)
	}
}
