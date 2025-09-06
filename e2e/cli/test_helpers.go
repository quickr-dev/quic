package e2e_cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	VMName = "quic-host"
)

func setupTestVM(t *testing.T) string {
	if vmExists(VMName) {
		t.Logf("Deleting existing VM %s...", VMName)
		err := exec.Command("multipass", "delete", "--purge", VMName).Run()
		require.NoError(t, err, err)
	}

	t.Logf("Creating VM %s...", VMName)
	err := exec.Command("timeout", "60", "multipass", "launch", "--name", VMName, "--disk", "15G", "--memory", "1G", "--cpus", "1").Run()
	require.NoError(t, err)

	setupSSHAccess(t, VMName)

	t.Logf("Setting up disks...")
	setupTestDisks(t, VMName)

	t.Logf("Getting VM IP...")
	ip := getVMIP(t, VMName)

	t.Logf("VM ready: %s", getVMIP(t, VMName))
	return ip
}

func vmExists(name string) bool {
	cmd := exec.Command("multipass", "info", name)
	return cmd.Run() == nil
}

func getVMIP(t *testing.T, name string) string {
	output, err := exec.Command("bash", "-c", fmt.Sprintf("multipass info %s | grep IPv4 | awk '{print $2}'", name)).Output()
	require.NoError(t, err)

	ip := strings.TrimSpace(string(output))
	require.True(t, ip != "")

	return ip
}

func setupSSHAccess(t *testing.T, vmName string) {
	t.Logf("Setting up SSH access...")

	// Create test SSH key pair and get the public key path
	keyPath := createTestSSHKey(t)
	pubKeyPath := keyPath + ".pub"

	// Add our test public key to VM's authorized_keys
	err := exec.Command("multipass", "transfer", pubKeyPath, vmName+":/tmp/test_key.pub").Run()
	require.NoError(t, err, err)

	commands := [][]string{
		{"multipass", "exec", vmName, "--", "bash", "-c", "cat /tmp/test_key.pub >> /home/ubuntu/.ssh/authorized_keys"},
		{"multipass", "exec", vmName, "--", "chmod", "600", "/home/ubuntu/.ssh/authorized_keys"},
		{"multipass", "exec", vmName, "--", "rm", "/tmp/test_key.pub"},
	}

	for _, cmdArgs := range commands {
		err := exec.Command(cmdArgs[0], cmdArgs[1:]...).Run()
		require.NoError(t, err, err)
	}

	// Add the test key to SSH agent for the quic CLI to use
	addKeyToSSHAgent(t, keyPath)
}

func createTestSSHKey(t *testing.T) string {
	// Create a temporary directory for test SSH keys
	testDir := filepath.Join(os.TempDir(), "quic-test-ssh")
	err := os.MkdirAll(testDir, 0700)
	require.NoError(t, err, err)

	// Only generate if it doesn't exist
	keyPath := filepath.Join(testDir, "id_rsa")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		err := exec.Command("ssh-keygen", "-t", "rsa", "-b", "2048", "-f", keyPath, "-N", "").Run()
		require.NoError(t, err, err)
		t.Logf("Generated test SSH key at %s", keyPath)
	}

	return keyPath
}

func addKeyToSSHAgent(t *testing.T, keyPath string) {
	t.Helper()

	// Start ssh-agent if not already running
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH agent not running, skipping SSH agent key addition")
	}

	// Add key to SSH agent
	cmd := exec.Command("ssh-add", keyPath)
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: Failed to add key to SSH agent: %v", err)
		t.Logf("Falling back to test key path detection")
	} else {
		t.Logf("Added test SSH key to SSH agent")
	}
}

func setupTestDisks(t *testing.T, vmName string) {
	commands := [][]string{
		{"multipass", "exec", vmName, "--", "sudo", "bash", "-c", "mkdir -p /tmp/test-disks"},
		{"timeout", "10", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "fallocate -l 128M /tmp/test-disks/disk1.img"},
		{"timeout", "10", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "fallocate -l 256M /tmp/test-disks/disk2.img"},
		{"timeout", "10", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "fallocate -l 512M /tmp/test-disks/disk3.img"},
		{"multipass", "exec", vmName, "--", "sudo", "bash", "-c", "losetup /dev/loop10 /tmp/test-disks/disk1.img"},
		{"multipass", "exec", vmName, "--", "sudo", "bash", "-c", "losetup /dev/loop11 /tmp/test-disks/disk2.img"},
		{"multipass", "exec", vmName, "--", "sudo", "bash", "-c", "losetup /dev/loop12 /tmp/test-disks/disk3.img"},
	}

	for _, cmdArgs := range commands {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		require.NoError(t, cmd.Run())
	}
}

func runQuicCommand(t *testing.T, args ...string) (string, error) {
	cmd := exec.Command("../../bin/quic", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runShellCommand(t *testing.T, command string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func cleanupQuicConfig(t *testing.T) {
	os.Remove("quic.json")
}

func requireFile(t *testing.T, path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to exist", path)
	}
}
