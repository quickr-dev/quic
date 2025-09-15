package e2e_cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runQuic(t *testing.T, args ...string) (string, error) {
	cmdArgs := append([]string{"../../bin/quic"}, args...)
	output, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput()
	if os.Getenv("DEBUG") != "" {
		t.Logf("$ %v", cmdArgs)
		t.Logf("↳ %s", string(output))
	}

	return string(output), err
}

// `quic host setup` downloads quicd from Github releases when running internal/cli/assets/base-setup.yml.
// To test local quicd code, we build and replace it in the VM.
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

func setupQuicCheckout(t *testing.T, vmName string) (checkoutOutput string, templateName string, branchName string, err error) {
	// Setup backup
	_, _, _, err = ensureCrunchyBridgeBackup(t, quicE2eClusterName)
	if err != nil {
		return "", "", "", fmt.Errorf("ensureCrunchyBridgeBackup failed: %w", err)
	}

	// Setup fresh VM
	vmIP := ensureFreshVM(t, vmName)

	// Setup host
	rmConfigFiles(t)
	runQuic(t, "host", "new", vmIP, "--devices", VMDevices)
	hostSetupOutput := runQuicHostSetupWithAck(t, []string{vmName})
	t.Log(hostSetupOutput)

	// Create user and login
	userOutput, err := runQuic(t, "user", "create", "Test User")
	if err != nil {
		return "", "", "", fmt.Errorf("quic user create failed: %w", err)
	}

	token := extractTokenFromCheckoutOutput(t, userOutput)
	if token == "" {
		return "", "", "", fmt.Errorf("token should be extracted from user create output")
	}

	_, err = runQuic(t, "login", "--token", token)
	if err != nil {
		return "", "", "", fmt.Errorf("quic login failed: %w", err)
	}

	// Create template
	templateName = fmt.Sprintf("test-%d", time.Now().UnixNano())
	_, err = runQuic(t, "template", "new", templateName,
		"--pg-version", "16",
		"--cluster-name", quicE2eClusterName,
		"--database", "quic_test")
	if err != nil {
		return "", "", "", fmt.Errorf("quic template new failed: %w", err)
	}

	// Setup template with API key from environment
	apiKey := getRequiredTestEnv("CB_API_KEY")
	if apiKey == "" {
		return "", "", "", fmt.Errorf("CB_API_KEY is required")
	}

	// Set CB_API_KEY environment variable for the command
	os.Setenv("CB_API_KEY", apiKey)
	defer os.Unsetenv("CB_API_KEY")

	t.Log("Running quic template setup...")
	templateSetupOutput, err := runQuic(t, "template", "setup")
	if err != nil {
		return "", "", "", fmt.Errorf("quic template setup failed: %s", templateSetupOutput)
	}
	t.Log(templateSetupOutput)
	t.Log("✓ Finished quic template setup")

	// Create branch
	branchName = fmt.Sprintf("test-branch-%d", time.Now().UnixNano())
	checkoutOutput, err = retryCheckoutUntilReady(t, branchName, templateName, 30*time.Second)
	if err != nil {
		return "", "", "", fmt.Errorf("quic checkout failed: %w", err)
	}

	return checkoutOutput, templateName, branchName, nil
}

func extractTokenFromCheckoutOutput(t *testing.T, output string) string {
	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		if strings.Contains(line, "$ quic login --token") {
			parts := strings.Fields(line)
			require.GreaterOrEqual(t, len(parts), 4, "Token line should have at least 4 parts")
			return parts[len(parts)-1] // Last part should be the token
		}
	}
	t.Fatal("Could not find token line in output")
	return ""
}

func retryCheckoutUntilReady(t *testing.T, branchName, templateName string, timeout time.Duration) (string, error) {
	startTime := time.Now()
	deadline := startTime.Add(timeout)
	interval := 3 * time.Second
	expectedErrorMessage := "template is still in recovery mode and not ready for branching"

	t.Log("Attempting to checkout branch")

	for time.Now().Before(deadline) {
		checkoutOutput, err := runQuic(t, "checkout", branchName, "--template", templateName)

		if err == nil {
			elapsed := time.Since(startTime)
			t.Logf("✓ Branch checkout succeeded after %v", elapsed)
			return checkoutOutput, nil
		}

		// Check both error message and command output for expected error
		if strings.Contains(checkoutOutput, expectedErrorMessage) || strings.Contains(err.Error(), expectedErrorMessage) {
			elapsed := time.Since(startTime).Round(time.Second)
			t.Logf("Template not ready yet (%v elapsed)", elapsed)
		} else {
			return "", fmt.Errorf("unexpected error during checkout: %s (output: %s)", err.Error(), strings.TrimSpace(checkoutOutput))
		}

		time.Sleep(interval)
	}

	return "", fmt.Errorf("checkout failed: template not ready after %v timeout", timeout)
}
