package e2e_cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
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

// quic host setup downloads quicd from Github releases,
// so we replace it with our local code for testing.
func runQuicHostSetupWithAck(t *testing.T, vmNames []string, args ...string) string {
	cmdArgs := append([]string{"host", "setup"}, args...)
	cmd := fmt.Sprintf("echo 'ack' | time ../../bin/quic %s", strings.Join(cmdArgs, " "))
	output := runShell(t, "bash", "-c", cmd)

	for _, vmName := range vmNames {
		// basic setup check
		output := runInVM(t, vmName, "zfs", "get", "-H", "-o", "value", "encryption", "tank")
		require.Equal(t, "aes-256-gcm\n", output, "tank should be encrypted with aes-256-gcm")

		replaceDownloadedQuicdWithLocalVersion(t, vmName)
	}

	return output
}

func runShell(t *testing.T, command string, args ...string) string {
	cmd := exec.Command(command, args...)

	if os.Getenv("DEBUG") != "" {
		t.Logf("$ %s %v", command, args)
		return runShellStreaming(t, cmd)
	}

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}

func runShellStreaming(t *testing.T, cmd *exec.Cmd) string {
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)

	var output strings.Builder
	done := make(chan bool)

	go func() {
		defer close(done)
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			line := scanner.Text()
			t.Log(line)
			output.WriteString(line + "\n")
		}
	}()

	err = cmd.Wait()
	<-done

	finalOutput := output.String()
	require.NoError(t, err, finalOutput)
	return finalOutput
}

func runInVM(t *testing.T, vmName string, command ...string) string {
	cmd := exec.Command("multipass", "exec", vmName, "--", "bash", "-c", strings.Join(command, " "))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return string(output)
}
