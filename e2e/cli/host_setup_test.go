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
		t.Run("validate ZFS setup", func(t *testing.T) {
			// Verify tank pool exists with specific properties
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "zfs", "list", "-H", "-o", "name,mountpoint", "tank")
			require.Equal(t, "tank\t/tank\n", output, "tank dataset should be named 'tank' and mounted at '/tank'")
			
			// Verify tank is encrypted with correct algorithm
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "zfs", "get", "-H", "-o", "value", "encryption", "tank")
			require.Equal(t, "aes-256-gcm\n", output, "tank should be encrypted with aes-256-gcm")

			// Verify /tank/data directory exists with correct permissions
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "ls", "-ld", "/tank/data")
			require.Contains(t, output, "postgres postgres", "/tank/data should be owned by postgres:postgres")
		})

		t.Run("validate system users and groups", func(t *testing.T) {
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "id", "quic")
			require.Contains(t, output, "uid=", "quic user should have a UID")
			require.Contains(t, output, "gid=", "quic user should have a GID")
			require.Contains(t, output, "postgres", "quic user should be in postgres group")
		})

		t.Run("validate directories and permissions", func(t *testing.T) {
			// Verify /etc/quic directory exists
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "ls", "-ld", "/etc/quic")
			require.Contains(t, output, "quic postgres", "/etc/quic should be owned by quic:postgres")

			// Verify /var/log/quic directory exists
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "ls", "-ld", "/var/log/quic")
			require.Contains(t, output, "quic quic", "/var/log/quic should be owned by quic:quic")

			// Verify TLS certificates exist
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "ls", "/etc/quic/certs/")
			require.Contains(t, output, "server.crt", "TLS certificate should exist")
			require.Contains(t, output, "server.key", "TLS key should exist")

			// Verify ZFS encryption key exists
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "ls", "-la", "/etc/quic/zfs-key")
			require.Contains(t, output, "root root", "ZFS key should be owned by root:root")
		})

		t.Run("validate services", func(t *testing.T) {
			// Verify quicd service is enabled and running
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "systemctl", "is-enabled", "quicd")
			require.Contains(t, output, "enabled", "quicd service should be enabled")

			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "systemctl", "is-active", "quicd")
			require.Contains(t, output, "active", "quicd service should be active")

			// Verify zfs-unlock service is enabled
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "systemctl", "is-enabled", "zfs-unlock")
			require.Contains(t, output, "enabled", "zfs-unlock service should be enabled")

			// Verify default postgresql service is disabled (expect exit code 1)
			output = runShellCommand(t, "bash", "-c", "multipass exec "+QuicHostVMName+" -- systemctl is-enabled postgresql || echo disabled")
			require.Contains(t, output, "disabled", "default postgresql service should be disabled")
		})

		t.Run("validate packages installed", func(t *testing.T) {
			// Verify ZFS utils are installed
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "which", "zpool")
			require.Contains(t, output, "/sbin/zpool", "zpool command should be available")

			// Verify PostgreSQL is installed
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "dpkg", "-l", "postgresql-16")
			require.Contains(t, output, "ii", "postgresql-16 should be installed")

			// Verify pgbackrest is installed
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "which", "pgbackrest")
			require.Contains(t, output, "/usr/bin/pgbackrest", "pgbackrest should be installed")
		})

		t.Run("validate quicd binary", func(t *testing.T) {
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "ls", "-la", "/usr/local/bin/quicd")
			require.Contains(t, output, "-rwxr-xr-x", "quicd binary should be executable")
			require.Contains(t, output, "root root", "quicd binary should be owned by root:root")

			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "/usr/local/bin/quicd", "--help")
			require.NotEmpty(t, output, "quicd binary should respond to --help")
		})

		t.Run("validate firewall configuration", func(t *testing.T) {
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "sudo", "ufw", "status")
			require.Contains(t, output, "Status: active", "UFW should be active")
			require.Contains(t, output, "22", "SSH port should be open")
			require.Contains(t, output, "8443", "gRPC port 8443 should be open")
		})

		t.Run("validate sudoers configuration", func(t *testing.T) {
			output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "sudo", "cat", "/etc/sudoers.d/quic-agent")
			require.Contains(t, output, "quic ALL=(root) NOPASSWD:", "quic sudoers should allow root commands")
			require.Contains(t, output, "quic ALL=(postgres) NOPASSWD:", "quic sudoers should allow postgres commands")
		})
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
