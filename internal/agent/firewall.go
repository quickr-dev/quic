package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

func openFirewallPort(port int) error {
	portSpec := fmt.Sprintf("%d/tcp", port)
	cmd := exec.Command("sudo", "ufw", "allow", portSpec)
	return cmd.Run()
}

func hasUFWRule(port int) bool {
	cmd := exec.Command("sudo", "ufw", "status")
	output, err := cmd.Output()
	if err != nil {
		return false // If we can't check UFW, assume no rule exists
	}

	portStr := fmt.Sprintf("%d/tcp", port)
	return strings.Contains(string(output), portStr)
}

func closeFirewallPort(port int) error {
	portSpec := fmt.Sprintf("%d/tcp", port)
	cmd := exec.Command("sudo", "ufw", "delete", "allow", portSpec)
	return cmd.Run()
}