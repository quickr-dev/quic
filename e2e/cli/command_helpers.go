package e2e_cli

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func runQuic(t *testing.T, args ...string) (string, error) {
	cmdArgs := append([]string{"../../bin/quic"}, args...)
	output, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput()

	return string(output), err
}

func runQuicHostSetupWithAck(t *testing.T, vmName string, args ...string) string {
	cmdArgs := append([]string{"host", "setup"}, args...)
	cmd := fmt.Sprintf("echo 'ack' | time ../../bin/quic %s", strings.Join(cmdArgs, " "))
	output := runShell(t, "bash", "-c", cmd)
	t.Log(output)
	reinstallQuicd(t, vmName)

	return output
}

func runShell(t *testing.T, command string, args ...string) string {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}

func runInVM(t *testing.T, vmName string, command ...string) string {
	cmd := exec.Command("multipass", "exec", vmName, "--", "bash", "-c", strings.Join(command, " "))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}
