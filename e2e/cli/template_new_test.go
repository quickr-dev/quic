package e2e_cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicTemplateNew(t *testing.T) {
	t.Run("template new requires name argument", func(t *testing.T) {
		output, err := runQuic(t, "template", "new")

		require.Error(t, err, "Expected command to fail without name argument")
		require.Contains(t, output, "accepts 1 arg(s), received 0", "Expected argument requirement message in output")
	})

	t.Run("successful template addition with flags", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, err := runQuic(t, "template", "new", "test-template", "--pg-version", "16", "--cluster-name", "test-cluster", "--database", "test_db")

		require.NoError(t, err, "quic template new should succeed\nOutput: %s", output)
		require.Contains(t, output, "Added template 'test-template'")

		requireFile(t, "quic.json")
		requireQuicConfigValue(t, "templates[0].name", "test-template")
		requireQuicConfigValue(t, "templates[0].pgVersion", "16")
		requireQuicConfigValue(t, "templates[0].database", "test_db")
		requireQuicConfigValue(t, "templates[0].provider.name", "crunchybridge")
		requireQuicConfigValue(t, "templates[0].provider.clusterName", "test-cluster")
	})

	t.Run("duplicate template name should fail", func(t *testing.T) {
		cleanupQuicConfig(t)

		// Add first template
		output, err := runQuic(t, "template", "new", "duplicate-template", "--cluster-name", "cluster1", "--database", "db1")
		require.NoError(t, err, "First template should succeed\nOutput: %s", output)

		// Try to add template with same name
		output, err = runQuic(t, "template", "new", "duplicate-template", "--cluster-name", "cluster2", "--database", "db2")
		require.Error(t, err, "Duplicate template should fail")
		require.Contains(t, output, "template with name duplicate-template already exists", "Expected duplicate name error message")
	})
}
