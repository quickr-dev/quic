package agent

import (
	"context"
	"fmt"
	"log"
	"os/exec"
)

func (s *AgentService) DeleteBranch(ctx context.Context, cloneName string, templateName string) (bool, error) {
	validatedName, err := ValidateBranchName(cloneName)
	if err != nil {
		return false, fmt.Errorf("invalid branch name: %w", err)
	}
	cloneName = validatedName

	// Check if template exists
	existingBranch, err := s.discoverBranchFromOS(templateName, cloneName)
	if err != nil {
		return false, fmt.Errorf("checking existing template: %w", err)
	}
	if existingBranch == nil {
		return false, nil
	}

	// Stop and remove systemd service
	serviceName := GetBranchServiceName(existingBranch.TemplateName, existingBranch.BranchName)
	if err := DeleteService(serviceName); err != nil {
		log.Printf("Warning: failed to remove systemd service for clone %s: %v", cloneName, err)
	}

	// Close firewall port
	if existingBranch.Port != "0" {
		if err := closeFirewallPort(existingBranch.Port); err != nil {
			log.Printf("Warning: failed to close firewall port %s: %v", existingBranch.Port, err)
		}
	}

	// Remove ZFS clone
	cloneDataset := branchDataset(templateName, cloneName)
	if datasetExists(cloneDataset) {
		if err := destroyZFSClone(cloneDataset); err != nil {
			return false, fmt.Errorf("destroying ZFS clone: %w", err)
		}
	}

	// Remove ZFS snapshot
	restoreDataset := templateDataset(templateName)
	snapshotName := restoreDataset + "@" + cloneName
	if snapshotExists(snapshotName) {
		if err := destroyZFSSnapshot(snapshotName); err != nil {
			return false, fmt.Errorf("destroying ZFS snapshot: %w", err)
		}
	}

	if err := auditEvent("checkout_delete", existingBranch); err != nil {
		log.Printf("Warning: failed to audit template deletion: %v", err)
	}

	return true, nil
}

func destroyZFSClone(cloneDataset string) error {
	cmd := exec.Command("sudo", "zfs", "destroy", cloneDataset)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("destroying ZFS clone %s: %w", cloneDataset, err)
	}

	auditEvent("zfs_clone_destroy", map[string]string{
		"clone_dataset": cloneDataset,
	})

	return nil
}

func destroyZFSSnapshot(snapshotName string) error {
	cmd := exec.Command("sudo", "zfs", "destroy", snapshotName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("destroying ZFS snapshot %s: %w", snapshotName, err)
	}

	auditEvent("zfs_snapshot_destroy", map[string]string{
		"snapshot_name": snapshotName,
	})

	return nil
}
