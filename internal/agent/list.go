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

	// List all datasets under the parent that are not "_restore"
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

		// Skip the _restore dataset
		if strings.HasSuffix(line, "/_restore") {
			continue
		}

		// Extract clone name from dataset path
		// Format should be: parent/_restore or parent/clonename
		parts := strings.Split(line, "/")
		if len(parts) < 2 {
			continue // Invalid format
		}
		
		cloneName := parts[len(parts)-1] // Last part is the clone name
		
		// Try to get checkout info for this clone
		checkout, err := s.discoverCheckoutFromOS(cloneName)
		if err != nil {
			// Log the error but continue with other checkouts
			fmt.Printf("Warning: failed to load checkout info for %s: %v\n", cloneName, err)
			continue
		}
		
		if checkout != nil {
			checkouts = append(checkouts, checkout)
		}
	}

	return checkouts, nil
}