package e2e_cli

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuicTemplateSetup(t *testing.T) {
	// Ensure CrunchyBridge cluster and backup exist
	ensureCrunchyBridgeBackup(t, quicE2eClusterName)
	vmIP := ensureFreshVM(t, QuicTemplateVMName)

	cleanupQuicConfig(t)

	// Setup host
	hostOutput, err := runQuic(t, "host", "new", vmIP, "--devices", "loop10,loop11")
	require.NoError(t, err, "quic host new should succeed\nOutput: %s", hostOutput)

	runShell(t, "bash", "-c", "echo 'ack' | ../../bin/quic host setup")

	// Build and deploy agent with RestoreTemplate support
	buildAndDeployAgent(t, QuicTemplateVMName)

	// Create template
	templateName := fmt.Sprintf("test-%d", time.Now().UnixNano())
	templateOutput, err := runQuic(t, "template", "new", templateName,
		"--pg-version", "16",
		"--cluster-name", quicE2eClusterName,
		"--database", "quic_test")
	require.NoError(t, err, "quic template new should succeed\nOutput: %s", templateOutput)

	// Verify template was added to config
	requireQuicConfigValue(t, "templates[0].name", templateName)
	requireQuicConfigValue(t, "templates[0].database", "quic_test")
	requireQuicConfigValue(t, "templates[0].provider.clusterName", quicE2eClusterName)

	// Setup template with API key from environment
	apiKey := getRequiredTestEnv("CB_API_KEY")
	require.NotEmpty(t, apiKey, "CB_API_KEY is required")

	// Set CB_API_KEY environment variable for the command
	os.Setenv("CB_API_KEY", apiKey)
	defer os.Unsetenv("CB_API_KEY")

	templateSetupOutput, err := runQuic(t, "template", "setup", templateName)
	require.NoError(t, err, "quic template setup should succeed\nOutput: %s", templateSetupOutput)

	// Verify setup success messages
	require.Contains(t, templateSetupOutput, "Setting up template")
	require.Contains(t, templateSetupOutput, "Found cluster:")
	require.Contains(t, templateSetupOutput, "Created backup token")
	require.Contains(t, templateSetupOutput, "Successfully setup 1 template(s)")

	// Verify ZFS dataset was created on the VM (tank/test-template)
	datasetName := fmt.Sprintf("tank/%s", templateName)
	datasetCheckOutput := runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "zfs", "list", datasetName)
	require.Contains(t, datasetCheckOutput, datasetName, "ZFS dataset should exist after template setup")
}
