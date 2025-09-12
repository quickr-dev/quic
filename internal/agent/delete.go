package agent

import (
	"context"
	"fmt"
	"log"
)

func (s *AgentService) DeleteBranch(ctx context.Context, branchName string, templateName string) (bool, error) {
	validatedName, err := ValidateBranchName(branchName)
	if err != nil {
		return false, fmt.Errorf("invalid branch name: %w", err)
	}
	branchName = validatedName

	// Check if template exists
	existingBranch, err := s.discoverBranchFromOS(templateName, branchName)
	if err != nil {
		return false, fmt.Errorf("checking existing template: %w", err)
	}
	if existingBranch == nil {
		return false, nil
	}

	// Stop and remove systemd service
	serviceName := GetBranchServiceName(existingBranch.TemplateName, existingBranch.BranchName)
	if err := DeleteService(serviceName); err != nil {
		log.Printf("Warning: failed to remove systemd service for clone %s: %v", branchName, err)
	}

	// Close firewall port
	if existingBranch.Port != "0" {
		if err := closeFirewallPort(existingBranch.Port); err != nil {
			log.Printf("Warning: failed to close firewall port %s: %v", existingBranch.Port, err)
		}
	}

	// Remove ZFS clone
	branchDataset := GetBranchDataset(templateName, branchName)
	if datasetExists(branchDataset) {
		if err := destroyDataset(branchDataset); err != nil {
			return false, err
		}
	}

	// Remove ZFS snapshot
	snapshotName := GetSnapshotName(templateName, branchName)
	if snapshotExists(snapshotName) {
		if err := destroySnapshot(snapshotName); err != nil {
			return false, err
		}
	}

	auditEvent("branch_delete", existingBranch)

	return true, nil
}
