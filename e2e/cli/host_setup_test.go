package e2e_cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicHostSetup(t *testing.T) {
	vmIP := ensureVMRunning(t, QuicHostVMName)

	t.Run("setup with single host", func(t *testing.T) {
		cleanupQuicConfig(t)

		// add vm as host in quic.json
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, output)

		// run quic setup
		output = runShellCommand(t, "bash", "-c", "echo 'ack' | ../../bin/quic host setup")

		// validate VM state
		output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "zfs", "list", "tank")
	})

	// t.Run("setup abort", func(t *testing.T) {
	// 	cmd := "echo 'no' | ../../bin/quic host setup"

	// 	_, _ = runShellCommand(t, cmd)

	// })

	// t.Run("setup with specific host", func(t *testing.T) {
	// 	cmd := "echo 'ack' | ../../bin/quic host setup --hosts default"

	// 	_, _ = runShellCommand(t, cmd)

	// })

	// t.Run("setup with invalid host", func(t *testing.T) {
	// 	_, _ = runQuicCommand(t, "host", "setup", "--hosts", "nonexistent")
	// })

	// t.Run("setup no hosts configured", func(t *testing.T) {
	// 	cleanupQuicConfig(t)

	// 	_, _ = runQuicCommand(t, "host", "setup")

	// })
}
