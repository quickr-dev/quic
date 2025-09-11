package agent

import (
	"context"
	"fmt"
	"log"
	"os/exec"
)

func (s *AgentService) DeleteBranch(ctx context.Context, cloneName string, templateName string) (bool, error) {
	// Validate and normalize clone name
	validatedName, err := ValidateCloneName(cloneName)
	if err != nil {
		return false, fmt.Errorf("invalid clone name: %w", err)
	}
	cloneName = validatedName

	// Check if checkout exists
	existing, err := s.discoverCheckoutFromOS(templateName, cloneName)
	if err != nil {
		return false, fmt.Errorf("checking existing checkout: %w", err)
	}
	if existing == nil {
		return false, nil // Nothing to delete
	}

	// Stop and remove systemd service for this clone
	serviceName := GetCloneServiceName(existing.TemplateName, existing.CloneName)
	if err := DeleteService(serviceName); err != nil {
		log.Printf("Warning: failed to remove systemd service for clone %s: %v", cloneName, err)
	}

	// Close firewall port
	if existing.Port > 0 {
		if err := closeFirewallPort(existing.Port); err != nil {
			log.Printf("Warning: failed to close firewall port %d: %v", existing.Port, err)
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

	// Audit log deletion
	if err := auditEvent("checkout_delete", existing); err != nil {
		log.Printf("Warning: failed to audit checkout deletion: %v", err)
	}

	return true, nil
}

func destroyZFSClone(cloneDataset string) error {
	cmd := exec.Command("sudo", "zfs", "destroy", cloneDataset)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("destroying ZFS clone %s: %w", cloneDataset, err)
	}

	// Audit ZFS clone destruction
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

	// Audit ZFS snapshot destruction
	auditEvent("zfs_snapshot_destroy", map[string]string{
		"snapshot_name": snapshotName,
	})

	return nil
}
