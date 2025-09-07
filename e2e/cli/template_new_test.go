package e2e_cli

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicTemplateNew(t *testing.T) {
	vmIP := ensureVMRunning(t, QuicTemplateVMName)

	// Setup host first
	t.Logf("Setting up template VM as a host...")
	cleanupQuicConfig(t)
	hostOutput, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
	require.NoError(t, err, "quic host new should succeed\nOutput: %s", hostOutput)

	setupOutput, err := runQuicCommand(t, "host", "setup")
	require.NoError(t, err, "quic host setup should succeed\nOutput: %s", setupOutput)

	t.Run("template new requires name argument", func(t *testing.T) {
		output, err := runQuicCommand(t, "template", "new")

		require.Error(t, err, "Expected command to fail without name argument")
		require.Contains(t, output, "accepts 1 arg(s), received 0", "Expected argument requirement message in output")
	})

	t.Run("successful template addition with flags", func(t *testing.T) {
		cleanupQuicConfig(t)

		// Re-setup host after cleaning config
		hostOutput, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, "quic host new should succeed\nOutput: %s", hostOutput)

		output, err := runQuicCommand(t, "template", "new", "test-template", "--pg-version", "16", "--cluster-name", "test-cluster")

		require.NoError(t, err, "quic template new should succeed\nOutput: %s", output)
		require.Contains(t, output, "Added template 'test-template'")

		requireFile(t, "quic.json")

		configContent, err := os.ReadFile("quic.json")
		require.NoError(t, err, "Failed to read quic.json")

		var config map[string]interface{}
		require.NoError(t, json.Unmarshal(configContent, &config), "Failed to parse quic.json")

		templates, ok := config["templates"].([]interface{})
		require.True(t, ok && len(templates) > 0, "Expected templates array in config")

		template := templates[0].(map[string]interface{})
		require.Equal(t, "test-template", template["name"], "Expected name 'test-template', got %s", template["name"])
		require.Equal(t, "16", template["pgVersion"], "Expected pgVersion '16', got %s", template["pgVersion"])

		provider := template["provider"].(map[string]interface{})
		require.Equal(t, "crunchybridge", provider["name"], "Expected provider name 'crunchybridge', got %s", provider["name"])
		require.Equal(t, "test-cluster", provider["clusterName"], "Expected cluster name 'test-cluster', got %s", provider["clusterName"])
	})

	t.Run("duplicate template name should fail", func(t *testing.T) {
		cleanupQuicConfig(t)

		// Re-setup host after cleaning config
		hostOutput, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, "quic host new should succeed\nOutput: %s", hostOutput)

		// Add first template
		output, err := runQuicCommand(t, "template", "new", "duplicate-template", "--cluster-name", "cluster1")
		require.NoError(t, err, "First template should succeed\nOutput: %s", output)

		// Try to add template with same name
		output, err = runQuicCommand(t, "template", "new", "duplicate-template", "--cluster-name", "cluster2")
		require.Error(t, err, "Duplicate template should fail")
		require.Contains(t, output, "template with name duplicate-template already exists", "Expected duplicate name error message")
	})
}
