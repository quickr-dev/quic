package e2e_cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuicTemplateSetup(t *testing.T) {
	ensureCrunchyBridgeBackup(t, quicE2eClusterName)
	vmIP := ensureFreshVM(t, QuicTemplateVMName)

	// Setup host
	t.Log("Rm quic.json")
	cleanupQuicConfig(t)
	t.Log("Running quic host new")
	runShell(t, "../../bin/quic", "host", "new", vmIP, "--devices", TestDevices)
	t.Log("Running quic host setup...")
	hostSetupOutput := runShell(t, "time", "bash", "-c", "echo 'ack' | ../../bin/quic host setup")
	t.Log(hostSetupOutput)
	t.Log("✓ Finished quic host setup")

	reinstallQuicd(t, QuicTemplateVMName)

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

	t.Log("Running quic template setup...")
	templateSetupOutput := runShell(t, "time", "../../bin/quic", "template", "setup", templateName)
	require.NoError(t, err, "quic template setup should succeed\nOutput: %s", templateSetupOutput)
	t.Log(templateSetupOutput)
	t.Log("✓ Finished quic template setup")

	// Verify setup success messages
	require.Contains(t, templateSetupOutput, "Setting up template")
	require.Contains(t, templateSetupOutput, "Found cluster:")
	require.Contains(t, templateSetupOutput, "Created backup token")
	require.Contains(t, templateSetupOutput, "Successfully setup 1 template(s)")

	// Verify ZFS dataset was created on the VM (tank/test-template)
	datasetName := fmt.Sprintf("tank/%s", templateName)
	datasetCheckOutput := runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "zfs", "list", datasetName)
	require.Contains(t, datasetCheckOutput, datasetName, "ZFS dataset should exist after template setup")

	// Verify template restore directory structure
	restoreMount := fmt.Sprintf("/opt/quic/%s/_restore", templateName)

	// Check that metadata file was created
	metadataFile := fmt.Sprintf("%s/.quic-init-meta.json", restoreMount)
	runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "test", "-f", metadataFile)

	// Read and verify metadata content
	metadataOutput := runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "cat", metadataFile)
	require.Contains(t, metadataOutput, templateName)
	require.Contains(t, metadataOutput, "port")
	require.Contains(t, metadataOutput, "service_name")

	// Verify PostgreSQL data directory was restored
	runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "test", "-d", restoreMount)
	runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "test", "-f", fmt.Sprintf("%s/postgresql.conf", restoreMount))
	runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "test", "-f", fmt.Sprintf("%s/PG_VERSION", restoreMount))

	// Verify PostgreSQL service was created and started
	serviceName := fmt.Sprintf("postgresql-%s", templateName)
	serviceStatusOutput := runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "systemctl", "is-active", serviceName)
	require.Contains(t, serviceStatusOutput, "active")

	// Extract port from metadata to test connection
	var metadata map[string]interface{}
	err = json.Unmarshal([]byte(metadataOutput), &metadata)
	require.NoError(t, err)
	port, ok := metadata["port"].(float64)
	require.True(t, ok, "port should be present in metadata")

	// Test PostgreSQL readiness
	runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "-u", "postgres", "pg_isready", "-p", fmt.Sprintf("%.0f", port))

	// Verify we can query the test data
	queryOutput := runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "-u", "postgres", "psql", "-p", fmt.Sprintf("%.0f", port), "-d", "quic_test", "-c", "SELECT COUNT(*) FROM users;")
	require.Contains(t, queryOutput, "5", "Should have 5 users from cloud-init setup")

	// standby.signal - Should exist
	standbySignalPath := fmt.Sprintf("%s/standby.signal", restoreMount)
	runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "test", "-f", standbySignalPath)

	// postgresql.auto.conf
	autoConfPath := fmt.Sprintf("%s/postgresql.auto.conf", restoreMount)
	autoConfOutput := runShell(t, "multipass", "exec", QuicTemplateVMName, "--", "sudo", "cat", autoConfPath)
	if !strings.Contains(autoConfOutput, "file not found") {
		require.NotContains(t, autoConfOutput, "# Clone instance - recovery disabled",
			"postgresql.auto.conf should not contain clone-specific configuration")
	}
}
