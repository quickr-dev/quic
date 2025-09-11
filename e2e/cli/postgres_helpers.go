package e2e_cli

import (
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

func tryParseTemplatePostmasterPid(t *testing.T, templateName string) (PostmasterPidData, bool) {
	t.Helper()

	templatePath := "/opt/quic/" + templateName + "/_restore"
	postmasterPidPath := templatePath + "/postmaster.pid"

	// Check if file exists
	cmd := exec.Command("multipass", "exec", QuicCheckoutVM, "--", "sudo", "test", "-f", postmasterPidPath)
	if err := cmd.Run(); err != nil {
		return PostmasterPidData{}, false
	}

	// get contents
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
