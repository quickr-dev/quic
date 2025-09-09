package e2e_cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuicHostSetup(t *testing.T) {
	quicHostIP := ensureFreshVM(t, QuicHostVM)
	quicHost2IP := ensureFreshVM(t, QuicHost2VM)

	t.Run("setup with single host", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices)
		require.NoError(t, err, output)

		// run once
		output = runQuicHostSetupWithAck(t, []string{QuicHostVM})
		require.Contains(t, output, "Setup completed: 1 successful")

		// can rerun just fine
		output = runQuicHostSetupWithAck(t, []string{QuicHostVM})
		require.Contains(t, output, "Setup completed: 1 successful")
	})

	t.Run("setup abort", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices)
		require.NoError(t, err, output)

		// Test aborting setup with 'no' input
		output = runShell(t, "bash", "-c", "echo 'no' | ../../bin/quic host setup")
		require.Contains(t, output, "Setup aborted", "Setup should be aborted when user enters 'no'")
	})

	t.Run("setup with specific host alias", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices, "--alias", "test-host")
		require.NoError(t, err, output)

		output = runQuicHostSetupWithAck(t, []string{QuicHostVM}, "--hosts", "test-host")
		require.Contains(t, output, "Setup completed: 1 successful")
		validateHostSetup(t, QuicHostVM)
	})

	t.Run("setup with specific host ip", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices)
		require.NoError(t, err, output)

		output = runQuicHostSetupWithAck(t, []string{QuicHostVM}, "--hosts", quicHostIP)
		require.Contains(t, output, "Setup completed: 1 successful")
		validateHostSetup(t, QuicHostVM)
	})

	t.Run("setup with invalid host", func(t *testing.T) {
		cleanupQuicConfig(t)
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices)
		require.NoError(t, err, output)

		// setup with non-existent host exists without error
		output, err = runQuic(t, "host", "setup", "--hosts", "nonexistent")
		require.NoError(t, err)

		// but display user message
		require.Contains(t, output, "Host 'nonexistent' not found", "Should show error for non-existent host")
	})

	t.Run("setup no hosts configured", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, err := runQuic(t, "host", "setup")
		require.Error(t, err, "Setup should fail with no hosts configured")
		require.Contains(t, output, "no hosts configured in quic.json")
	})

	t.Run("setup with multiple hosts configured requires --hosts", func(t *testing.T) {
		cleanupQuicConfig(t)

		// Add two hosts
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices, "--alias", "host1")
		require.NoError(t, err, output)
		output, err = runQuic(t, "host", "new", quicHost2IP, "--devices", VMDevices, "--alias", "host2")
		require.NoError(t, err, output)

		// Try setup without specifying hosts - should prompt for safety
		output, err = runQuic(t, "host", "setup")
		require.NoError(t, err, "Command should succeed but prompt for host specification")
		require.Contains(t, output, "For safety, please specify the hosts to setup", "Should prompt to specify hosts for safety")
	})

	t.Run("sets up multiple hosts with --hosts all", func(t *testing.T) {
		cleanupQuicConfig(t)

		// Add two hosts
		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices, "--alias", "host1")
		require.NoError(t, err, output)
		output, err = runQuic(t, "host", "new", quicHost2IP, "--devices", VMDevices, "--alias", "host2")
		require.NoError(t, err, output)

		// Setup all hosts
		output = runQuicHostSetupWithAck(t, []string{QuicHostVM, QuicHost2VM}, "--hosts", "all")
		require.Contains(t, output, "Setup completed:", "Setup should complete for all hosts")

		// Validate complete setup on both hosts using reusable function
		validateHostSetup(t, QuicHostVM)
		validateHostSetup(t, QuicHost2VM)
	})

	t.Run("duplicate alias validation", func(t *testing.T) {
		cleanupQuicConfig(t)

		output, err := runQuic(t, "host", "new", quicHostIP, "--devices", VMDevices, "--alias", "samealias")
		require.NoError(t, err, output)

		output, err = runQuic(t, "host", "new", quicHost2IP, "--devices", VMDevices, "--alias", "samealias")
		require.Error(t, err, "Second host with same alias should fail")
		require.Contains(t, output, "host with alias samealias already exists", "Should show duplicate alias error")
	})
}

func validateHostSetup(t *testing.T, vmName string) {
	t.Run("validate ZFS setup", func(t *testing.T) {
		// Verify tank pool exists with specific properties
		output := runShell(t, "multipass", "exec", vmName, "--", "zfs", "list", "-H", "-o", "name,mountpoint", "tank")
		require.Equal(t, "tank\t/tank\n", output, "tank dataset should be named 'tank' and mounted at '/tank'")

		// Verify tank is encrypted with correct algorithm
		output = runShell(t, "multipass", "exec", vmName, "--", "zfs", "get", "-H", "-o", "value", "encryption", "tank")
		require.Equal(t, "aes-256-gcm\n", output, "tank should be encrypted with aes-256-gcm")

		// Verify /tank/data directory exists with correct permissions
		output = runShell(t, "multipass", "exec", vmName, "--", "ls", "-ld", "/tank/data")
		require.Contains(t, output, "postgres postgres", "/tank/data should be owned by postgres:postgres")
	})

	t.Run("validate system users and groups", func(t *testing.T) {
		output := runShell(t, "multipass", "exec", vmName, "--", "id", "quic")
		require.Contains(t, output, "uid=", "quic user should have a UID")
		require.Contains(t, output, "gid=", "quic user should have a GID")
		require.Contains(t, output, "postgres", "quic user should be in postgres group")
	})

	t.Run("validate directories and permissions", func(t *testing.T) {
		// Verify /etc/quic directory exists
		output := runShell(t, "multipass", "exec", vmName, "--", "ls", "-ld", "/etc/quic")
		require.Contains(t, output, "quic postgres", "/etc/quic should be owned by quic:postgres")

		// Verify /var/log/quic directory exists
		output = runShell(t, "multipass", "exec", vmName, "--", "ls", "-ld", "/var/log/quic")
		require.Contains(t, output, "quic quic", "/var/log/quic should be owned by quic:quic")

		// Verify TLS certificates exist
		output = runShell(t, "multipass", "exec", vmName, "--", "ls", "/etc/quic/certs/")
		require.Contains(t, output, "server.crt", "TLS certificate should exist")
		require.Contains(t, output, "server.key", "TLS key should exist")

		// Verify ZFS encryption key exists
		output = runShell(t, "multipass", "exec", vmName, "--", "ls", "-la", "/etc/quic/zfs-key")
		require.Contains(t, output, "root root", "ZFS key should be owned by root:root")
	})

	t.Run("validate services", func(t *testing.T) {
		// Verify quicd service is enabled and running
		output := runShell(t, "multipass", "exec", vmName, "--", "systemctl", "is-enabled", "quicd")
		require.Contains(t, output, "enabled", "quicd service should be enabled")

		output = runShell(t, "multipass", "exec", vmName, "--", "systemctl", "is-active", "quicd")
		require.Contains(t, output, "active", "quicd service should be active")

		// Verify zfs-unlock service is enabled
		output = runShell(t, "multipass", "exec", vmName, "--", "systemctl", "is-enabled", "zfs-unlock")
		require.Contains(t, output, "enabled", "zfs-unlock service should be enabled")

		// Verify default postgresql service is disabled (expect exit code 1)
		output = runShell(t, "bash", "-c", "multipass exec "+vmName+" -- systemctl is-enabled postgresql || echo disabled")
		require.Contains(t, output, "disabled", "default postgresql service should be disabled")
	})

	t.Run("validate packages installed", func(t *testing.T) {
		// Verify ZFS utils are installed
		output := runShell(t, "multipass", "exec", vmName, "--", "which", "zpool")
		require.Contains(t, output, "/sbin/zpool", "zpool command should be available")

		// Verify PostgreSQL is installed
		output = runShell(t, "multipass", "exec", vmName, "--", "dpkg", "-l", "postgresql-16")
		require.Contains(t, output, "ii", "postgresql-16 should be installed")

		// Verify pgbackrest is installed
		output = runShell(t, "multipass", "exec", vmName, "--", "which", "pgbackrest")
		require.Contains(t, output, "/usr/bin/pgbackrest", "pgbackrest should be installed")
	})

	t.Run("validate quicd binary", func(t *testing.T) {
		output := runShell(t, "multipass", "exec", vmName, "--", "ls", "-la", "/usr/local/bin/quicd")
		require.Contains(t, output, "-rwxr-xr-x", "quicd binary should be executable")
		require.Contains(t, output, "root root", "quicd binary should be owned by root:root")
	})

	t.Run("validate firewall configuration", func(t *testing.T) {
		output := runShell(t, "multipass", "exec", vmName, "--", "sudo", "ufw", "status")
		require.Contains(t, output, "Status: active", "UFW should be active")
		require.Contains(t, output, "22", "SSH port should be open")
		require.Contains(t, output, "8443", "gRPC port 8443 should be open")
	})

	t.Run("validate sudoers configuration", func(t *testing.T) {
		output := runShell(t, "multipass", "exec", vmName, "--", "sudo", "cat", "/etc/sudoers.d/quic-agent")
		require.Contains(t, output, "quic ALL=(root) NOPASSWD:", "quic sudoers should allow root commands")
		require.Contains(t, output, "quic ALL=(postgres) NOPASSWD:", "quic sudoers should allow postgres commands")
	})

	t.Run("validate sqlite database", func(t *testing.T) {
		// ensure quicd is active
		output := runShell(t, "multipass", "exec", vmName, "--", "systemctl", "status", "quicd")
		t.Logf("quicd service status: %s", output)

		// Verify database file exists (created when quicd service starts)
		output = runShell(t, "multipass", "exec", vmName, "--", "ls", "-la", "/etc/quic/db.sqlite")
		require.Contains(t, output, "/etc/quic/db.sqlite", "SQLite database file should exist after quicd service starts")

		// Verify users table exists with correct schema
		output = runShell(t, "multipass", "exec", vmName, "--", "sqlite3", "/etc/quic/db.sqlite", ".schema users")
		require.Contains(t, output, "CREATE TABLE", "Users table should exist")
		require.Contains(t, output, "name TEXT NOT NULL UNIQUE", "Users table should have name column")
		require.Contains(t, output, "token TEXT NOT NULL", "Users table should have token column")
		require.Contains(t, output, "created_at DATETIME DEFAULT CURRENT_TIMESTAMP", "Users table should have created_at column")
	})
}
