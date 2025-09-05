package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ListBranches discovers and returns information about all existing checkouts
// If restoreName is provided, only returns checkouts from that specific restore
func (s *AgentService) ListBranches(ctx context.Context, restoreName string) ([]*CheckoutInfo, error) {
	var searchDataset string
	if restoreName != "" {
		// If specific restore name provided, search only within that restore
		searchDataset = ZFSParentDataset + "/" + restoreName
	} else {
		// If no restore name provided, search all restores
		searchDataset = ZFSParentDataset
	}

	// List all datasets recursively under the search dataset
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", "-r", searchDataset)
	output, err := cmd.Output()
	if err != nil {
		// If the specific restore doesn't exist, return empty list instead of error
		if restoreName != "" {
			return []*CheckoutInfo{}, nil
		}
		return nil, fmt.Errorf("listing ZFS datasets: %w", err)
	}

	var checkouts []*CheckoutInfo
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == searchDataset {
			continue // Skip empty lines and search dataset itself
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

		datasetRestoreName := parts[len(parts)-2] // Second to last part is the restore name
		cloneName := parts[len(parts)-1]          // Last part is the clone name

		// Try to get checkout info for this clone
		checkout, err := s.discoverCheckoutFromOS(datasetRestoreName, cloneName)
		if err != nil {
			// Log the error but continue with other checkouts
			fmt.Printf("Warning: failed to load checkout info for %s/%s: %v\n", datasetRestoreName, cloneName, err)
			continue
		}

		if checkout != nil {
			checkouts = append(checkouts, checkout)
		}
	}

	return checkouts, nil
}
