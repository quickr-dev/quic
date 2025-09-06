package e2e_cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicHostNew(t *testing.T) {
	vmIP := ensureVMRunning(t, QuicHostVMName)

	t.Run("successful host addition", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")

		require.NoError(t, err, "quic host new should succeed\nOutput: %s", output)

		require.Contains(t, output, "✓ Testing SSH connection")
		require.Contains(t, output, "✓ Discovering block devices")
		require.Contains(t, output, "✓ Using specified devices")
		require.Contains(t, output, "✓ Added host")

		requireFile(t, "quic.json")

		configContent, err := os.ReadFile("quic.json")
		if err != nil {
			t.Fatalf("Failed to read quic.json: %v", err)
		}

		var config map[string]interface{}
		if err := json.Unmarshal(configContent, &config); err != nil {
			t.Fatalf("Failed to parse quic.json: %v", err)
		}

		hosts, ok := config["hosts"].([]interface{})
		if !ok || len(hosts) == 0 {
			t.Fatal("Expected hosts array in config")
		}

		host := hosts[0].(map[string]interface{})
		if host["ip"] != vmIP {
			t.Errorf("Expected IP %s, got %s", vmIP, host["ip"])
		}
		if host["alias"] != "default" {
			t.Errorf("Expected alias 'default', got %s", host["alias"])
		}
	})

	t.Run("invalid IP address", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, err := runQuicCommand(t, "host", "new", "invalid-ip")

		if err == nil {
			t.Fatal("Expected command to fail with invalid IP")
		}

		if !strings.Contains(output, "failed to connect") {
			t.Error("Expected connection failure message in output")
		}
	})

	t.Run("unreachable host", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, err := runQuicCommand(t, "host", "new", "192.168.99.99")

		if err == nil {
			t.Fatal("Expected command to fail with unreachable host")
		}

		if !strings.Contains(output, "failed to connect") {
			t.Error("Expected connection failure message in output")
		}
	})

	t.Run("host new requires IP argument", func(t *testing.T) {
		output, err := runQuicCommand(t, "host", "new")

		if err == nil {
			t.Fatal("Expected command to fail without IP argument")
		}

		if !strings.Contains(output, "accepts 1 arg(s), received 0") {
			t.Error("Expected argument requirement message in output")
		}
	})
}
