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
	SnapshotName       = "base"
	QuicHostVMName     = "quic-host"
	QuicHost2VMName    = "quic-host2"
	QuicTemplateVMName = "quic-template"
)

func ensureVMRunning(t *testing.T, vmName string) string {
	if vmExists(t, vmName) {
		startVM(t, vmName)
		return getVMIP(t, vmName)
	}

	return recreateVM(t, vmName)
}

func ensureFreshVM(t *testing.T, vmName string) string {
	if vmExists(t, vmName) && snapshotExists(t, vmName, SnapshotName) {
		stopVM(t, vmName)
		restoreVM(t, vmName, SnapshotName)
		startVM(t, vmName)
	} else {
		recreateVM(t, vmName)
		createSnapshot(t, vmName, SnapshotName)
	}
	setupTestDisks(t, vmName)

	return getVMIP(t, vmName)
}

func recreateVM(t *testing.T, vmName string) string {
	if vmExists(t, vmName) {
		deleteVM(t, vmName)
	}
	launchVM(t, vmName)
	setupSSHAccess(t, vmName)
	setupTestDisks(t, vmName)

	return getVMIP(t, vmName)
}

func vmExists(t *testing.T, name string) bool {
	cmd := exec.Command("multipass", "info", name)
	return cmd.Run() == nil
}

func getVMIP(t *testing.T, name string) string {
	output := runShell(t, "bash", "-c", fmt.Sprintf("multipass info %s | grep IPv4 | awk '{print $2}'", name))

	ip := strings.TrimSpace(output)
	require.True(t, ip != "")

	return ip
}

func setupSSHAccess(t *testing.T, vmName string) {
	t.Logf("Setting up SSH access...")

	// Create test SSH key pair and get the public key path
	keyPath := createTestSSHKey(t)
	pubKeyPath := keyPath + ".pub"

	// Add our test public key to VM's authorized_keys
	runShell(t, "multipass", "transfer", pubKeyPath, vmName+":/tmp/test_key.pub")

	commands := [][]string{
		{"multipass", "exec", vmName, "--", "bash", "-c", "cat /tmp/test_key.pub >> /home/ubuntu/.ssh/authorized_keys"},
		{"multipass", "exec", vmName, "--", "chmod", "600", "/home/ubuntu/.ssh/authorized_keys"},
		{"multipass", "exec", vmName, "--", "rm", "/tmp/test_key.pub"},
	}

	for _, cmdArgs := range commands {
		runShell(t, cmdArgs[0], cmdArgs[1:]...)
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
		runShell(t, "ssh-keygen", "-t", "rsa", "-b", "2048", "-f", keyPath, "-N", "")
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
		{"timeout", "5", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "fallocate -l 100M /tmp/test-disks/disk1.img"},
		{"timeout", "5", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "fallocate -l 100M /tmp/test-disks/disk2.img"},
		{"timeout", "5", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "fallocate -l 100M /tmp/test-disks/disk3.img"},
		{"timeout", "5", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "losetup /dev/loop10 /tmp/test-disks/disk1.img"},
		{"timeout", "5", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "losetup /dev/loop11 /tmp/test-disks/disk2.img"},
		{"timeout", "5", "multipass", "exec", vmName, "--", "sudo", "bash", "-c", "losetup /dev/loop12 /tmp/test-disks/disk3.img"},
	}

	for _, cmdArgs := range commands {
		t.Logf("Running '%v'", cmdArgs)
		runShell(t, cmdArgs[0], cmdArgs[1:]...)
	}
	t.Log("✓ Setup disks done")
}

func snapshotExists(t *testing.T, vmName, snapshotName string) bool {
	output := runShell(t, "multipass", "info", vmName, "--snapshots")
	return strings.Contains(output, snapshotName)
}

func stopVM(t *testing.T, vmName string) {
	t.Logf("Stopping VM %s...", vmName)
	runShell(t, "multipass", "stop", vmName)
}

func startVM(t *testing.T, vmName string) {
	t.Logf("Starting VM %s...", vmName)
	runShell(t, "multipass", "start", vmName)
}

func launchVM(t *testing.T, vmName string) {
	t.Logf("Creating VM %s...", vmName)
	runShell(t, "timeout", "60", "multipass", "launch", "--name", vmName, "--disk", "15G", "--memory", "1G", "--cpus", "1")
}

func deleteVM(t *testing.T, vmName string) {
	t.Logf("Deleting VM %s...", vmName)
	runShell(t, "multipass", "delete", "--purge", vmName)
}

func restoreVM(t *testing.T, vmName, snapshotName string) {
	t.Logf("Restoring VM %s from snapshot %s...", vmName, snapshotName)
	runShell(t, "multipass", "restore", vmName+"."+snapshotName, "--destructive")
}

func createSnapshot(t *testing.T, vmName, snapshotName string) {
	t.Logf("Creating base snapshot...")
	stopVM(t, vmName)
	runShell(t, "multipass", "snapshot", vmName, "--name", snapshotName)
	startVM(t, vmName)
}

func cloneVM(t *testing.T, sourceVM, destVM string) {
	t.Logf("Cloning VM %s to %s...", sourceVM, destVM)
	stopVM(t, sourceVM)
	runShell(t, "multipass", "clone", sourceVM, "--name", destVM)
	startVM(t, sourceVM)
	startVM(t, destVM)
}

func ensureClonedVM(t *testing.T, sourceVM, destVM string) string {
	if vmExists(t, destVM) {
		startVM(t, destVM)
		return getVMIP(t, destVM)
	}

	// Clone from source VM
	cloneVM(t, sourceVM, destVM)
	setupTestDisks(t, destVM)
	return getVMIP(t, destVM)
}

func reinstallQuicd(t *testing.T, vmName string) {
	t.Log("Reinstalling agent...")

	// Detect VM architecture
	archOutput := runShell(t, "multipass", "exec", vmName, "--", "uname", "-m")
	vmArch := strings.TrimSpace(archOutput)

	// Map VM architecture to Go GOARCH
	var goArch string
	switch vmArch {
	case "x86_64":
		goArch = "amd64"
	case "aarch64":
		goArch = "arm64"
	default:
		t.Fatalf("Unsupported VM architecture: %s", vmArch)
	}

	runShell(t, "timeout", "5s", "bash", "-c", fmt.Sprintf("cd ../../ && GOOS=linux GOARCH=%s go build -o bin/quicd-linux ./cmd/quicd", goArch))
	runShell(t, "timeout", "5s", "multipass", "transfer", "../../bin/quicd-linux", vmName+":/tmp/quicd")
	runShell(t, "timeout", "5s", "bash", "-c", fmt.Sprintf("multipass exec %s -- sudo systemctl stop quicd || true", vmName))
	runShell(t, "timeout", "5s", "multipass", "exec", vmName, "--", "sudo", "mv", "/tmp/quicd", "/usr/local/bin/quicd")
	runShell(t, "timeout", "5s", "multipass", "exec", vmName, "--", "sudo", "chown", "root:root", "/usr/local/bin/quicd")
	runShell(t, "timeout", "5s", "multipass", "exec", vmName, "--", "sudo", "chmod", "+x", "/usr/local/bin/quicd")
	runShell(t, "timeout", "5s", "multipass", "exec", vmName, "--", "sudo", "systemctl", "enable", "quicd")
	runShell(t, "timeout", "5s", "multipass", "exec", vmName, "--", "sudo", "systemctl", "start", "quicd")

	t.Log("✓ Agent reinstalled")
}
