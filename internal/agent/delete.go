package agent

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

func (s *AgentService) DeleteBranch(ctx context.Context, template string, branchName string) (bool, error) {
	branchName, err := ValidateBranchName(branchName)
	if err != nil {
		return false, fmt.Errorf("invalid branch name: %w", err)
	}

	// Check if template exists
	branch, err := s.getBranchMetadata(GetBranchDataset(template, branchName))
	if err != nil {
		return false, fmt.Errorf("checking existing template: %w", err)
	}
	if branch != nil {
		if err := closeFirewallPort(branch.Port); err != nil {
			log.Printf("Warning: failed to close firewall port %s: %v", branch.Port, err)
		}
	}

	// Stop and remove systemd service
	serviceName := GetBranchServiceName(template, branchName)
	if ServiceExists(serviceName) {
		if err := DeleteService(serviceName); err != nil {
			log.Printf("Warning: failed to remove systemd service for clone %s: %v", branchName, err)
		}
	}

	snapshotName := GetSnapshotName(template, branchName)
	if snapshotExists(snapshotName) {
		// -R to destroy the snapshot and its clones
		if err := destroyDataset(snapshotName, "-R"); err != nil {
			return false, err
		}
	}

	mountpoint := GetBranchMountpoint(template, branchName)
	output, err := exec.Command("sudo", "rmdir", mountpoint).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "No such file or directory") {
		return false, fmt.Errorf("failed to remove mountpoint %s: %v", mountpoint, err)
	}

	auditEvent("branch_delete", branch)

	return true, nil
}
