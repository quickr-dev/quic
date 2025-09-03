package agent

import (
	"context"
	"fmt"
	"log"
	"os/exec"
)

func (s *CheckoutService) DeleteCheckout(ctx context.Context, cloneName string, restoreName string) (bool, error) {
	// Validate and normalize clone name
	validatedName, err := ValidateCloneName(cloneName)
	if err != nil {
		return false, fmt.Errorf("invalid clone name: %w", err)
	}
	cloneName = validatedName

	zfsConfig := &ZFSConfig{
		ParentDataset: s.config.ZFSParentDataset,
		RestoreName:   restoreName,
	}

	// Check if checkout exists
	existing, err := s.discoverCheckoutFromOS(zfsConfig, cloneName)
	if err != nil {
		return false, fmt.Errorf("checking existing checkout: %w", err)
	}
	if existing == nil {
		return false, nil // Nothing to delete
	}

	// Stop and remove systemd service for this clone
	if err := s.removeSystemdService(existing); err != nil {
		log.Printf("Warning: failed to remove systemd service for clone %s: %v", cloneName, err)
	}

	// Close firewall port
	if existing.Port > 0 {
		if err := s.closeFirewallPort(existing.Port); err != nil {
			log.Printf("Warning: failed to close firewall port %d: %v", existing.Port, err)
		}
	}

	// Note: metadata file cleanup is handled automatically by ZFS clone destruction

	// Remove ZFS clone
	cloneDataset := zfsConfig.CloneDataset(cloneName)
	if s.datasetExists(cloneDataset) {
		if err := s.destroyZFSClone(cloneDataset); err != nil {
			return false, fmt.Errorf("destroying ZFS clone: %w", err)
		}
	}

	// Remove ZFS snapshot
	restoreDataset := zfsConfig.RestoreDataset()
	snapshotName := restoreDataset + "@" + cloneName
	if s.snapshotExists(snapshotName) {
		if err := s.destroyZFSSnapshot(snapshotName); err != nil {
			return false, fmt.Errorf("destroying ZFS snapshot: %w", err)
		}
	}

	// Audit log deletion
	if err := s.auditEvent("checkout_delete", existing); err != nil {
		log.Printf("Warning: failed to audit checkout deletion: %v", err)
	}

	return true, nil
}

func (s *CheckoutService) destroyZFSClone(cloneDataset string) error {
	cmd := exec.Command("sudo", "zfs", "destroy", cloneDataset)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("destroying ZFS clone %s: %w", cloneDataset, err)
	}

	// Audit ZFS clone destruction
	s.auditEvent("zfs_clone_destroy", map[string]string{
		"clone_dataset": cloneDataset,
	})

	return nil
}

func (s *CheckoutService) destroyZFSSnapshot(snapshotName string) error {
	cmd := exec.Command("sudo", "zfs", "destroy", snapshotName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("destroying ZFS snapshot %s: %w", snapshotName, err)
	}

	// Audit ZFS snapshot destruction
	s.auditEvent("zfs_snapshot_destroy", map[string]string{
		"snapshot_name": snapshotName,
	})

	return nil
}
