package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

func openFirewallPort(port string) error {
	portSpec := fmt.Sprintf("%s/tcp", port)
	cmd := exec.Command("sudo", "ufw", "allow", portSpec)
	return cmd.Run()
}

func hasUFWRule(port string) bool {
	cmd := exec.Command("sudo", "ufw", "status")
	output, err := cmd.Output()
	if err != nil {
		return false // If we can't check UFW, assume no rule exists
	}

	portStr := fmt.Sprintf("%s/tcp", port)
	return strings.Contains(string(output), portStr)
}

func closeFirewallPort(port string) error {
	portSpec := fmt.Sprintf("%s/tcp", port)
	cmd := exec.Command("sudo", "ufw", "delete", "allow", portSpec)
	return cmd.Run()
}
