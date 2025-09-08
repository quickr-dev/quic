package e2e_cli

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func runQuic(t *testing.T, args ...string) (string, error) {
	cmdArgs := append([]string{"../../bin/quic"}, args...)
	output, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput()

	return string(output), err
}

func runShell(t *testing.T, command string, args ...string) string {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}

func runInVM(t *testing.T, vmName string, args ...string) string {
	cmd := exec.Command("multipass", "exec", vmName, "--", "bash", "-c", fmt.Sprintf("su quic -c '%s'", args))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}
