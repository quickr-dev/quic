package e2e_cli

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type MultipassInfo struct {
	Info map[string]struct {
		IPv4 []string `json:"ipv4"`
	} `json:"info"`
}

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

func backupConfig(t *testing.T) string {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	configPath := fmt.Sprintf("%s/.config/quic/config.json", homeDir)

	// Read existing config if it exists
	if data, err := os.ReadFile(configPath); err == nil {
		return string(data)
	}
	return ""
}

func restoreConfig(t *testing.T, originalConfig string) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	configPath := fmt.Sprintf("%s/.config/quic/config.json", homeDir)

	if originalConfig == "" {
		// Remove config if it didn't exist before
		os.Remove(configPath)
		return
	}

	// Restore original config
	err = os.WriteFile(configPath, []byte(originalConfig), 0644)
	require.NoError(t, err)
}

func createTestConfig(t *testing.T, vmIP string) {
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	configDir := fmt.Sprintf("%s/.config/quic", homeDir)
	configPath := fmt.Sprintf("%s/config.json", configDir)

	// Ensure config directory exists
	err = os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create test config pointing to VM
	testConfig := fmt.Sprintf(`{
  "selected_server": "%s",
  "last_server_check": "%s",
  "servers": {
    "%s": {
      "last_latency_ms": 50,
      "last_success": "%s"
    }
  }
}`, vmIP, time.Now().Format(time.RFC3339), vmIP, time.Now().Format(time.RFC3339))

	err = os.WriteFile(configPath, []byte(testConfig), 0644)
	require.NoError(t, err)
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}