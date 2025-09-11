package e2e_cli

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// PostmasterPidData represents the contents of a postmaster.pid file
type PostmasterPidData struct {
	PID           string
	DataDirectory string
	StartTime     string
	Port          int
}

func parseTemplatePostmasterPid(t *testing.T, templateName string) (PostmasterPidData, bool) {
	templatePath := "/opt/quic/" + templateName + "/_restore"
	postmasterPidPath := templatePath + "/postmaster.pid"
	return parsePostmasterPid(t, postmasterPidPath)
}

func parseBranchPostmasterPid(t *testing.T, templateName, branchName string) (PostmasterPidData, bool) {
	clonePath := "/opt/quic/" + templateName + "/" + branchName
	postmasterPidPath := clonePath + "/postmaster.pid"
	return parsePostmasterPid(t, postmasterPidPath)
}

func psqlTemplate(t *testing.T, templateName, query string) (string, error) {
	pidData, ok := parseTemplatePostmasterPid(t, templateName)
	if !ok {
		return "", fmt.Errorf("failed to parse postmaster.pid for template %s", templateName)
	}

	cmd := exec.Command("multipass", "exec", QuicCheckoutVM, "--", "sudo", "-u", "postgres", "psql",
		"--no-align", "--tuples-only", "-p", strconv.Itoa(pidData.Port), "-d", "quic_test", "-c", query)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql command failed: %w (output: %s)", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

func psqlBranch(t *testing.T, templateName, branchName, query string) string {
	pidData, ok := parseBranchPostmasterPid(t, templateName, branchName)
	if !ok {
		t.Fatalf("Failed to parse postmaster.pid for branch %s/%s", templateName, branchName)
	}

	return runInVM(t, QuicCheckoutVM, "sudo", "-u", "postgres", "psql",
		"--no-align", "--tuples-only", "-p", strconv.Itoa(pidData.Port), "-d", "quic_test", "-c", "\""+query+"\"")
}

func parsePostmasterPid(t *testing.T, postmasterPidPath string) (PostmasterPidData, bool) {
	cmd := exec.Command("multipass", "exec", QuicCheckoutVM, "--", "sudo", "test", "-f", postmasterPidPath)
	if err := cmd.Run(); err != nil {
		return PostmasterPidData{}, false
	}

	content := runInVM(t, QuicCheckoutVM, "sudo", "cat", postmasterPidPath)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	if len(lines) < 4 {
		return PostmasterPidData{}, false
	}

	result := PostmasterPidData{}
	result.PID = strings.TrimSpace(lines[0])
	result.DataDirectory = strings.TrimSpace(lines[1])
	result.StartTime = strings.TrimSpace(lines[2])

	portStr := strings.TrimSpace(lines[3])
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return PostmasterPidData{}, false
	}
	result.Port = port

	return result, true
}
