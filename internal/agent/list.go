package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ListCheckouts discovers and returns information about all existing checkouts
func (s *CheckoutService) ListCheckouts(ctx context.Context) ([]*CheckoutInfo, error) {
	zfsConfig := &ZFSConfig{
		ParentDataset: s.config.ZFSParentDataset,
	}

	// List all datasets recursively under the parent
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", "-r", zfsConfig.ParentDataset)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing ZFS datasets: %w", err)
	}

	var checkouts []*CheckoutInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == zfsConfig.ParentDataset {
			continue // Skip empty lines and parent dataset
		}

		// Skip the _restore datasets
		if strings.HasSuffix(line, "/_restore") {
			continue
		}

		// Extract clone name and restore name from dataset path
		// Expected format: tank/RESTORE_NAME/CLONE_NAME
		parts := strings.Split(line, "/")
		if len(parts) < 3 {
			continue // Invalid format - need at least tank/restore/clone
		}

		restoreName := parts[len(parts)-2] // Second to last part is the restore name
		cloneName := parts[len(parts)-1]   // Last part is the clone name

		// Create ZFS config with the restore name for discovery
		cloneZfsConfig := &ZFSConfig{
			ParentDataset: s.config.ZFSParentDataset,
			RestoreName:   restoreName,
		}

		// Try to get checkout info for this clone
		checkout, err := s.discoverCheckoutFromOS(cloneZfsConfig, cloneName)
		if err != nil {
			// Log the error but continue with other checkouts
			fmt.Printf("Warning: failed to load checkout info for %s/%s: %v\n", restoreName, cloneName, err)
			continue
		}

		if checkout != nil {
			checkouts = append(checkouts, checkout)
		}
	}

	return checkouts, nil
}
