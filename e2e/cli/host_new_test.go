package e2e_cli

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicHostNew(t *testing.T) {
	vmIP := ensureVMRunning(t, QuicHostVM)

	t.Run("successful host addition", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuic(t, "host", "new", vmIP, "--devices", VMDevices)

		require.NoError(t, err, "quic host new should succeed\nOutput: %s", output)

		require.Contains(t, output, "Added host")

		requireFile(t, "quic.json")

		configContent, err := os.ReadFile("quic.json")
		require.NoError(t, err, "Failed to read quic.json")

		var config map[string]interface{}
		require.NoError(t, json.Unmarshal(configContent, &config), "Failed to parse quic.json")

		hosts, ok := config["hosts"].([]interface{})
		require.True(t, ok && len(hosts) > 0, "Expected hosts array in config")

		host := hosts[0].(map[string]interface{})
		require.Equal(t, vmIP, host["ip"], "Expected IP %s, got %s", vmIP, host["ip"])
		require.Equal(t, "default", host["alias"], "Expected alias 'default', got %s", host["alias"])
	})

	t.Run("invalid IP address", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, err := runQuic(t, "host", "new", "invalid-ip")

		require.Error(t, err, "Expected command to fail with invalid IP")
		require.Contains(t, output, "failed to connect", "Expected connection failure message in output")
	})

	t.Run("host new requires IP argument", func(t *testing.T) {
		output, err := runQuic(t, "host", "new")

		require.Error(t, err, "Expected command to fail without IP argument")
		require.Contains(t, output, "accepts 1 arg(s), received 0", "Expected argument requirement message in output")
	})
}
