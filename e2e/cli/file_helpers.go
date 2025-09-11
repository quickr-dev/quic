package e2e_cli

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func cleanupQuicConfig(t *testing.T) {
	os.Remove("quic.json")
	
	// Also clean up user config to avoid stale IP addresses
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userConfigPath := homeDir + "/.config/quic/config.json"
		os.Remove(userConfigPath)
	}
}

func requireFile(t *testing.T, path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to exist", path)
	}
}

func requireQuicConfigValue(t *testing.T, jsonPath string, expected interface{}) {
	t.Helper()

	configContent, err := os.ReadFile("quic.json")
	require.NoError(t, err, "Failed to read quic.json")

	var config map[string]interface{}
	require.NoError(t, json.Unmarshal(configContent, &config), "Failed to parse quic.json")

	value := getValueByPath(t, config, jsonPath)
	require.Equal(t, expected, value, "Expected %s to be %v, got %v", jsonPath, expected, value)
}

// Simple path parser for common patterns like "templates[0].name"
// This handles: key, key[index], key[index].subkey
func getValueByPath(t *testing.T, data interface{}, path string) interface{} {
	t.Helper()

	if path == "" {
		return data
	}

	// Split on dots
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		if strings.Contains(part, "[") {
			// Handle array access like "templates[0]"
			keyEnd := strings.Index(part, "[")
			key := part[:keyEnd]
			indexStr := part[keyEnd+1 : len(part)-1] // Remove [ and ]
			index, err := strconv.Atoi(indexStr)
			require.NoError(t, err, "Invalid array index: %s", indexStr)

			// Get the array
			obj, ok := current.(map[string]interface{})
			require.True(t, ok, "Expected object for key %s", key)
			arr, ok := obj[key].([]interface{})
			require.True(t, ok, "Expected array for key %s", key)
			require.True(t, index < len(arr), "Index %d out of bounds for array %s", index, key)

			current = arr[index]
		} else {
			// Handle simple key access
			obj, ok := current.(map[string]interface{})
			require.True(t, ok, "Expected object for key %s", part)
			current = obj[part]
		}
	}

	return current
}
