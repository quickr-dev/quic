package e2e_cli

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func runQuicCommand(t *testing.T, args ...string) (string, error) {
	cmdArgs := append([]string{"../../bin/quic"}, args...)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func runShellCommand(t *testing.T, command string, args ...string) string {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}