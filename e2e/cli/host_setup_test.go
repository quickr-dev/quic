package e2e_cli

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// validateHostSetup performs comprehensive validation of a host setup
func validateHostSetup(t *testing.T, vmName string) {
	t.Run("validate ZFS setup", func(t *testing.T) {
		// Verify tank pool exists with specific properties
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "zfs", "list", "-H", "-o", "name,mountpoint", "tank")
		require.Equal(t, "tank\t/tank\n", output, "tank dataset should be named 'tank' and mounted at '/tank'")

		// Verify tank is encrypted with correct algorithm
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "zfs", "get", "-H", "-o", "value", "encryption", "tank")
		require.Equal(t, "aes-256-gcm\n", output, "tank should be encrypted with aes-256-gcm")

		// Verify /tank/data directory exists with correct permissions
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "ls", "-ld", "/tank/data")
		require.Contains(t, output, "postgres postgres", "/tank/data should be owned by postgres:postgres")
	})

	t.Run("validate system users and groups", func(t *testing.T) {
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "id", "quic")
		require.Contains(t, output, "uid=", "quic user should have a UID")
		require.Contains(t, output, "gid=", "quic user should have a GID")
		require.Contains(t, output, "postgres", "quic user should be in postgres group")
	})

	t.Run("validate directories and permissions", func(t *testing.T) {
		// Verify /etc/quic directory exists
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "ls", "-ld", "/etc/quic")
		require.Contains(t, output, "quic postgres", "/etc/quic should be owned by quic:postgres")

		// Verify /var/log/quic directory exists
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "ls", "-ld", "/var/log/quic")
		require.Contains(t, output, "quic quic", "/var/log/quic should be owned by quic:quic")

		// Verify TLS certificates exist
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "ls", "/etc/quic/certs/")
		require.Contains(t, output, "server.crt", "TLS certificate should exist")
		require.Contains(t, output, "server.key", "TLS key should exist")

		// Verify ZFS encryption key exists
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "ls", "-la", "/etc/quic/zfs-key")
		require.Contains(t, output, "root root", "ZFS key should be owned by root:root")
	})

	t.Run("validate services", func(t *testing.T) {
		// Verify quicd service is enabled and running
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "systemctl", "is-enabled", "quicd")
		require.Contains(t, output, "enabled", "quicd service should be enabled")

		output = runShellCommand(t, "multipass", "exec", vmName, "--", "systemctl", "is-active", "quicd")
		require.Contains(t, output, "active", "quicd service should be active")

		// Verify zfs-unlock service is enabled
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "systemctl", "is-enabled", "zfs-unlock")
		require.Contains(t, output, "enabled", "zfs-unlock service should be enabled")

		// Verify default postgresql service is disabled (expect exit code 1)
		output = runShellCommand(t, "bash", "-c", "multipass exec "+vmName+" -- systemctl is-enabled postgresql || echo disabled")
		require.Contains(t, output, "disabled", "default postgresql service should be disabled")
	})

	t.Run("validate packages installed", func(t *testing.T) {
		// Verify ZFS utils are installed
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "which", "zpool")
		require.Contains(t, output, "/sbin/zpool", "zpool command should be available")

		// Verify PostgreSQL is installed
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "dpkg", "-l", "postgresql-16")
		require.Contains(t, output, "ii", "postgresql-16 should be installed")

		// Verify pgbackrest is installed
		output = runShellCommand(t, "multipass", "exec", vmName, "--", "which", "pgbackrest")
		require.Contains(t, output, "/usr/bin/pgbackrest", "pgbackrest should be installed")
	})

	t.Run("validate quicd binary", func(t *testing.T) {
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "ls", "-la", "/usr/local/bin/quicd")
		require.Contains(t, output, "-rwxr-xr-x", "quicd binary should be executable")
		require.Contains(t, output, "root root", "quicd binary should be owned by root:root")

		output = runShellCommand(t, "multipass", "exec", vmName, "--", "/usr/local/bin/quicd", "--help")
		require.NotEmpty(t, output, "quicd binary should respond to --help")
	})

	t.Run("validate firewall configuration", func(t *testing.T) {
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "sudo", "ufw", "status")
		require.Contains(t, output, "Status: active", "UFW should be active")
		require.Contains(t, output, "22", "SSH port should be open")
		require.Contains(t, output, "8443", "gRPC port 8443 should be open")
	})

	t.Run("validate sudoers configuration", func(t *testing.T) {
		output := runShellCommand(t, "multipass", "exec", vmName, "--", "sudo", "cat", "/etc/sudoers.d/quic-agent")
		require.Contains(t, output, "quic ALL=(root) NOPASSWD:", "quic sudoers should allow root commands")
		require.Contains(t, output, "quic ALL=(postgres) NOPASSWD:", "quic sudoers should allow postgres commands")
	})
}

func TestQuicHostSetup(t *testing.T) {
	vmIP := ensureVMRunning(t, QuicHostVMName)

	t.Run("setup with single host", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, output)

		// run quic setup
		output = runShellCommand(t, "bash", "-c", "echo 'ack' | ../../bin/quic host setup")

		// validate VM state using reusable function
		validateHostSetup(t, QuicHostVMName)
	})

	t.Run("setup abort", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, output)

		// Test aborting setup with 'no' input
		output = runShellCommand(t, "bash", "-c", "echo 'no' | ../../bin/quic host setup")
		require.Contains(t, output, "Setup aborted", "Setup should be aborted when user enters 'no'")
	})

	t.Run("setup with specific host alias", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11", "--alias", "test-host")
		require.NoError(t, err, output)

		// Setup with specific host alias
		output = runShellCommand(t, "bash", "-c", "echo 'ack' | ../../bin/quic host setup --hosts test-host")
		require.Contains(t, output, "Setup completed:", "Setup should complete successfully with specific host alias")
	})

	t.Run("setup with specific host ip", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, output)

		// Setup with specific host IP
		output = runShellCommand(t, "bash", "-c", fmt.Sprintf("echo 'ack' | ../../bin/quic host setup --hosts %s", vmIP))
		require.Contains(t, output, "Setup completed:", "Setup should complete successfully with specific host IP")

		// Validate ZFS dataset exists
		output = runShellCommand(t, "multipass", "exec", QuicHostVMName, "--", "zfs", "list", "-H", "-o", "name", "tank")
		require.Equal(t, "tank\n", output, "tank dataset should exist after setup")
	})

	t.Run("setup with invalid host", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11")
		require.NoError(t, err, output)

		// Try to setup with non-existent host (should print error but exit successfully)
		output, err = runQuicCommand(t, "host", "setup", "--hosts", "nonexistent")
		require.NoError(t, err, "Command should exit successfully but show error message")
		require.Contains(t, output, "Host 'nonexistent' not found", "Should show error for non-existent host")
	})

	t.Run("setup no hosts configured", func(t *testing.T) {
		cleanupQuicConfig(t)

		// Try to setup with no hosts configured
		output, err := runQuicCommand(t, "host", "setup")
		require.Error(t, err, "Setup should fail with no hosts configured")
		require.Contains(t, output, "no hosts configured in quic.json", "Should show error for no hosts configured")
	})

	t.Run("setup with multiple hosts configured requires --hosts", func(t *testing.T) {
		cleanupQuicConfig(t)
		cloneVMIP := ensureClonedVM(t, QuicHostVMName, QuicHost2VMName)

		// Add two hosts with different aliases
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11", "--alias", "host1")
		require.NoError(t, err, output)
		output, err = runQuicCommand(t, "host", "new", cloneVMIP, "--devices", "loop10,loop11", "--alias", "host2")
		require.NoError(t, err, output)

		// Try setup without specifying hosts - should prompt for safety
		output, err = runQuicCommand(t, "host", "setup")
		require.NoError(t, err, "Command should succeed but prompt for host specification")
		require.Contains(t, output, "For safety, please specify the hosts to setup", "Should prompt to specify hosts for safety")
	})

	t.Run("sets up multiple hosts with --hosts all", func(t *testing.T) {
		cleanupQuicConfig(t)
		cloneVMIP := ensureClonedVM(t, QuicHostVMName, QuicHost2VMName)

		// Add two hosts with different aliases
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11", "--alias", "host1")
		require.NoError(t, err, output)
		output, err = runQuicCommand(t, "host", "new", cloneVMIP, "--devices", "loop10,loop11", "--alias", "host2")
		require.NoError(t, err, output)

		// Setup all hosts
		output = runShellCommand(t, "bash", "-c", "echo 'ack' | ../../bin/quic host setup --hosts all")
		require.Contains(t, output, "Setup completed:", "Setup should complete for all hosts")

		// Validate complete setup on both hosts using reusable function
		validateHostSetup(t, QuicHostVMName)
		validateHostSetup(t, QuicHost2VMName)
	})

	t.Run("duplicate alias validation", func(t *testing.T) {
		cleanupQuicConfig(t)
		cloneVMIP := ensureClonedVM(t, QuicHostVMName, QuicHost2VMName)

		// Add first host
		output, err := runQuicCommand(t, "host", "new", vmIP, "--devices", "loop10,loop11", "--alias", "samealias")
		require.NoError(t, err, output)

		// Try to add second host with same alias (should fail)
		output, err = runQuicCommand(t, "host", "new", cloneVMIP, "--devices", "loop10,loop11", "--alias", "samealias")
		require.Error(t, err, "Second host with same alias should fail")
		require.Contains(t, output, "host with alias samealias already exists", "Should show duplicate alias error")
	})
}
